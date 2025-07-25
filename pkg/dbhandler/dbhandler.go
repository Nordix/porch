// Copyright 2025 The Nephio Authors
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

package dbhandler

import (
	"context"
	"database/sql"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/klog/v2"

	cachetypes "github.com/nephio-project/porch/pkg/cache/types"
)

type DBHandler struct {
	DBCacheOptions cachetypes.DBCacheOptions
	DataSource     string
	Db             dbSQLInterface
}

var DbHandler *DBHandler = nil

var tracer = otel.Tracer("dbHandler")

func OpenDB(ctx context.Context, opts cachetypes.CacheOptions) error {
	_, span := tracer.Start(ctx, "DBHandler::OpenDB", trace.WithAttributes())
	defer span.End()

	klog.V(4).Infof("OpenDB: %+v %+v", opts.DBCacheOptions.Driver, opts.DBCacheOptions.DataSource)

	if DbHandler != nil {
		klog.V(4).Infof("OpenDB: database %q, already open", opts.DBCacheOptions.DataSource)
		return nil
	}

	db, err := sql.Open(opts.DBCacheOptions.Driver, opts.DBCacheOptions.DataSource)
	if err != nil {
		klog.V(4).Infof("OpenDB: database %q open failed: %q", opts.DBCacheOptions.DataSource, err)
		return err
	}

	if err := db.Ping(); err != nil {
		db.Close()
		klog.V(4).Infof("OpenDB: database %q open failed", opts.DBCacheOptions.DataSource)
		return err
	}

	klog.V(4).Infof("OpenDB: database %q opened", opts.DBCacheOptions.DataSource)

	DbHandler = &DBHandler{
		DBCacheOptions: opts.DBCacheOptions,
		Db:             db,
	}

	return nil
}

func GetDB() *DBHandler {
	if DbHandler == nil {
		klog.Errorf("GetDB: the database is not open")
		return nil
	}

	return DbHandler
}

func CloseDB(ctx context.Context) error {
	_, span := tracer.Start(ctx, "DBHandler::CloseDB", trace.WithAttributes())
	defer span.End()

	if DbHandler == nil {
		klog.Infof("CloseDB: the databse is already closed")
		return nil
	}

	var err error
	if err = DbHandler.Db.Close(); err == nil {
		klog.V(4).Infof("CloseDB: database %q closed", DbHandler.DataSource)
	} else {
		klog.V(4).Infof("CloseDB: close failed to database %q: %q", DbHandler.DataSource, err)
	}

	DbHandler = nil
	return err
}
