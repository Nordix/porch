/*
 Copyright 2025 The Nephio Authors.

 Licensed under the Apache License, Version 2.0 (the "License");
 You may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package starlark

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/qri-io/starlib/util"
	"go.starlark.net/resolve"
	"go.starlark.net/starlark"
	"sigs.k8s.io/kustomize/kyaml/errors"
	"sigs.k8s.io/kustomize/kyaml/fn/runtime/runtimeutil"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/kio/filters"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

// Filter transforms a set of resources through the provided program
type Filter struct {
	Name string

	// Program is a starlark script which will be run against the resources
	Program string

	// URL is the url of a starlark program to fetch and run
	URL string

	// Path is the path to a starlark program to read and run
	Path string

	runtimeutil.FunctionFilter
}

func (sf *Filter) String() string {
	return fmt.Sprintf(
		"name: %v path: %v url: %v program: %v", sf.Name, sf.Path, sf.URL, sf.Program)
}

func (sf *Filter) Filter(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
	err := sf.setup()
	if err != nil {
		return nil, err
	}
	sf.FunctionFilter.Run = sf.Run

	return sf.FunctionFilter.Filter(nodes)
}

func (sf *Filter) setup() error {
	if (sf.URL != "" && sf.Path != "") ||
		(sf.URL != "" && sf.Program != "") ||
		(sf.Path != "" && sf.Program != "") {
		return errors.Errorf("Filter Path, Program and URL are mutually exclusive")
	}

	// read the program from a file
	if sf.Path != "" {
		b, err := os.ReadFile(sf.Path)
		if err != nil {
			return err
		}
		sf.Program = string(b)
	}

	// read the program from a URL
	if sf.URL != "" {
		err := func() error {
			resp, err := http.Get(sf.URL)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			sf.Program = string(b)
			return nil
		}()
		if err != nil {
			return err
		}
	}

	return nil
}

func (sf *Filter) Run(reader io.Reader, writer io.Writer) error {
	// retain map of inputs to outputs by id so if the name is changed by the
	// starlark program, we are able to match the same resources
	value, err := sf.readResourceList(reader)
	if err != nil {
		return errors.Wrap(err)
	}

	err = runStarlark(sf.Name, sf.Program, value)
	if err != nil {
		return errors.Wrap(err)
	}

	return sf.writeResourceList(value, writer)
}

// runStarlark runs the starlark script
func runStarlark(name, starlarkProgram string, resourceList starlark.Value) error {
	// Enabled some non-standard starlark features (https://pkg.go.dev/go.starlark.net/resolve#pkg-variables).
	// LoadBindsGlobally is not enabled, since it has been deprecated.
	resolve.AllowSet = true
	resolve.AllowGlobalReassign = true
	resolve.AllowRecursion = true

	// run the starlark as program as transformation function
	thread := &starlark.Thread{Name: name, Load: load}

	ctx := &Context{resourceList: resourceList}
	pd, err := ctx.predeclared()
	if err != nil {
		return errors.Wrap(err)
	}
	_, err = starlark.ExecFile(thread, name, starlarkProgram, pd)
	if err != nil {
		return errors.Wrap(err)
	}
	return nil
}

// inputToResourceList transforms input into a starlark.Value
func (sf *Filter) readResourceList(reader io.Reader) (starlark.Value, error) {
	// read and parse the inputs
	rl := bytes.Buffer{}
	_, err := rl.ReadFrom(reader)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	rn, err := yaml.Parse(rl.String())
	if err != nil {
		return nil, errors.Wrap(err)
	}
	return rnodeToStarlarkValue(rn)
}

// rnodeToStarlarkValue converts a RNode to a starlark value.
func rnodeToStarlarkValue(rn *yaml.RNode) (starlark.Value, error) {
	m, err := rn.Map()
	if err != nil {
		return nil, errors.Wrap(err)
	}
	return util.Marshal(m) // convert to starlark value
}

// starlarkValueToRNode converts the output of the starlark program to a RNode.
func starlarkValueToRNode(value starlark.Value) (*yaml.RNode, error) {
	// convert the modified resourceList back into a slice of RNodes
	// by first converting to a map[string]interface{}
	out, err := util.Unmarshal(value)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	b, err := yaml.Marshal(out)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	return yaml.Parse(string(b))
}

// writeResourceList converts the output of the starlark program to bytes and
// write to the writer.
func (sf *Filter) writeResourceList(value starlark.Value, writer io.Writer) error {
	rl, err := starlarkValueToRNode(value)
	if err != nil {
		return errors.Wrap(err)
	}

	// preserve the comments from the input
	items, err := rl.Pipe(yaml.Lookup("items"))
	if err != nil {
		return errors.Wrap(err)
	}
	err = items.VisitElements(func(node *yaml.RNode) error {
		// starlark will serialize the resources sorting the fields alphabetically,
		// format them to have a better ordering
		_, err := filters.FormatFilter{}.Filter([]*yaml.RNode{node})
		return err
	})
	if err != nil {
		return errors.Wrap(err)
	}

	s, err := rl.String()
	if err != nil {
		return errors.Wrap(err)
	}

	_, err = writer.Write([]byte(s))
	return err
}

// SimpleFilter transforms a set of resources through the provided starlark
// program. It doesn't touch the id annotation. It doesn't copy comments.
type SimpleFilter struct {
	// Name of the starlark program
	Name string
	// Program is a starlark script which will be run against the resources
	Program string
	// FunctionConfig is the functionConfig for the function.
	FunctionConfig *yaml.RNode
}

func (sf *SimpleFilter) String() string {
	return fmt.Sprintf(
		"name: %v program: %v", sf.Name, sf.Program)
}

func (sf *SimpleFilter) Filter(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
	in, err := WrapResources(nodes, sf.FunctionConfig)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	value, err := rnodeToStarlarkValue(in)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	err = runStarlark(sf.Name, sf.Program, value)
	if err != nil {
		return nil, errors.Wrap(err)
	}

	rn, err := starlarkValueToRNode(value)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	updatedNodes, _, err := UnwrapResources(rn)
	return updatedNodes, err
}

// WrapResources wraps resources and an optional functionConfig in a resourceList
func WrapResources(nodes []*yaml.RNode, fc *yaml.RNode) (*yaml.RNode, error) {
	var ynodes []*yaml.Node
	for _, rnode := range nodes {
		ynodes = append(ynodes, rnode.YNode())
	}
	m := map[string]interface{}{
		"apiVersion": kio.ResourceListAPIVersion,
		"kind":       kio.ResourceListKind,
		"items":      []interface{}{},
	}
	out, err := yaml.FromMap(m)
	if err != nil {
		return nil, err
	}
	_, err = out.Pipe(
		yaml.Lookup("items"),
		yaml.Append(ynodes...))
	if err != nil {
		return nil, err
	}
	if fc != nil {
		_, err = out.Pipe(
			yaml.SetField("functionConfig", fc))
		if err != nil {
			return nil, err
		}
	}

	return out, nil
}

// UnwrapResources unwraps the resources and the functionConfig from a resourceList
func UnwrapResources(in *yaml.RNode) ([]*yaml.RNode, *yaml.RNode, error) {
	items, err := in.Pipe(yaml.Lookup("items"))
	if err != nil {
		return nil, nil, errors.Wrap(err)
	}
	nodes, err := items.Elements()
	if err != nil {
		return nil, nil, errors.Wrap(err)
	}
	fc, err := in.Pipe(yaml.Lookup("functionConfig"))
	if err != nil {
		return nil, nil, errors.Wrap(err)
	}
	return nodes, fc, nil
}
