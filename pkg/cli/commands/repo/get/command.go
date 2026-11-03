// Copyright 2022,2025-2026 The kpt Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package get

import (
	"context"
	"strings"

	"github.com/kptdev/kpt/pkg/lib/errors"
	"github.com/kptdev/kpt/pkg/lib/util/cmdutil"
	cliutils "github.com/kptdev/porch/internal/cliutils"
	"github.com/kptdev/porch/pkg/cli/commands/repo/docs"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/cmd/get"
	kubectlcmdutil "k8s.io/kubectl/pkg/cmd/util"
)

const (
	command      = "cmdrepoget"
	resourceName = "repositories.config.porch.kpt.dev"
)

func NewCommand(ctx context.Context, rcg *genericclioptions.ConfigFlags) *cobra.Command {
	return newRunner(ctx, rcg).Command
}

func newRunner(ctx context.Context, rcg *genericclioptions.ConfigFlags) *runner {
	r := &runner{
		ctx:        ctx,
		getFlags:   cmdutil.Options{ConfigFlags: rcg},
		printFlags: get.NewGetPrintFlags(),
	}
	c := &cobra.Command{
		Use:     "get [REPOSITORY_NAME]",
		Aliases: []string{"ls", "list"},
		Short:   docs.GetShort,
		Long:    docs.GetShort + "\n" + docs.GetLong,
		Example: docs.GetExamples,
		PreRunE: r.preRunE,
		RunE:    r.runE,
		Hidden:  cliutils.HidePorchCommands,
	}
	r.Command = c

	// Create flags
	r.getFlags.AddFlags(c)
	r.printFlags.AddFlags(c)
	// Mirror `kubectl get --chunk-size`: page large Repository lists instead of
	// buffering the whole collection in one response.
	r.chunkSize = kubectlcmdutil.DefaultChunkSize
	kubectlcmdutil.AddChunkSizeFlag(c, &r.chunkSize)
	return r
}

type runner struct {
	ctx     context.Context
	Command *cobra.Command

	// Flags
	getFlags   cmdutil.Options
	printFlags *get.PrintFlags

	// chunkSize is the page size used when listing repositories; 0 disables chunking.
	chunkSize int64

	requestTable bool
}

func (r *runner) preRunE(cmd *cobra.Command, _ []string) error {
	outputOption := cmd.Flags().Lookup("output").Value.String()
	if strings.Contains(outputOption, "custom-columns") || outputOption == "yaml" || strings.Contains(outputOption, "json") {
		r.requestTable = false
	} else {
		r.requestTable = true
	}

	allNamespacesFlag := cmd.Flags().Lookup("all-namespaces").Value.String()
	if strings.Contains(allNamespacesFlag, "true") {
		r.printFlags.HumanReadableFlags.WithNamespace = true
	} else {
		r.printFlags.HumanReadableFlags.WithNamespace = false
	}
	return nil
}

func (r *runner) runE(cmd *cobra.Command, args []string) error {
	const op errors.Op = command + ".runE"

	// For some reason our use of k8s libraries result in error when decoding
	// RepositoryList when we use strongly typed data. Therefore for now we
	// use unstructured communication.
	// The error is: `no kind "RepositoryList" is registered for the internal
	// version of group "config.porch.kpt.dev" in scheme`. Of course there _is_
	// no such kind since CRDs seem to have only versioned resources.
	b, err := r.getFlags.ResourceBuilder()
	if err != nil {
		return err
	}

	// TODO: Support table mode over proto
	// TODO: Print namespace in multi-namespace mode
	b = b.Unstructured()

	if len(args) > 0 {
		b.ResourceNames(resourceName, args...)
	} else {
		b = b.SelectAllParam(true).
			ResourceTypes(resourceName)
	}

	b = b.ContinueOnError().Latest().Flatten()

	if r.chunkSize > 0 {
		b = b.RequestChunksOf(r.chunkSize)
	}

	if r.requestTable {
		b = b.TransformRequests(func(req *rest.Request) {
			req.SetHeader("Accept", strings.Join([]string{
				"application/json;as=Table;g=meta.k8s.io;v=v1",
				"application/json",
			}, ","))
		})
	}
	res := b.Do()
	if err := res.Err(); err != nil {
		return errors.E(op, err)
	}

	printer, err := r.printFlags.ToPrinter()
	if err != nil {
		return errors.E(op, err)
	}
	if r.requestTable {
		// Decode each server-side Table page into the custom Repository columns
		// before the human printer renders it. The TablePrinter passes
		// non-Table objects through untouched, so it is safe to keep wrapped for
		// every page. Reusing this ONE instance across pages is what makes the
		// column header print on the first page and be suppressed on the rest
		// (the delegate tracks lastType per instance).
		printer = &get.TablePrinter{Delegate: printer}
	}

	w := printers.GetNewTabWriter(cmd.OutOrStdout())
	defer func() { _ = w.Flush() }()

	// Stream page-by-page: RequestChunksOf pages the server (res.Visit follows
	// the continue token) and each page is printed and discarded here, so peak
	// client memory is one chunk rather than the whole Repository set -- on the
	// table path as well as yaml/json/name/custom-columns. res.Visit hands the
	// table path the per-page *Table object, which the TablePrinter decodes;
	// earlier breakage came from printing that Table through the bare printer,
	// bypassing the TablePrinter and degrading to generic NAME/CREATED-AT columns.
	if err := res.Visit(func(i *resource.Info, err error) error {
		if err != nil {
			return err
		}
		return printer.PrintObj(i.Object, w)
	}); err != nil {
		return errors.E(op, err)
	}

	return nil
}
