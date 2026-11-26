// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/factory"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/types"
)

// useStandaloneDatabase reports whether the chain should be stored in a
// standalone database. A chain already stored in db is never moved.
func (c config) useStandaloneDatabase(log logging.Logger, db database.Database) bool {
	if rawdb.ReadCanonicalHash(types.NewChainEthDB(db), genesisNumber) == (common.Hash{}) {
		return c.UseStandaloneDatabase
	}
	if c.UseStandaloneDatabase {
		log.Warn(`ignoring "use-standalone-database" for existing database`)
	}
	return false
}

// standaloneDatabase opens the database configured by c, registering its
// metrics with snowCtx. The caller MUST close the returned database.
func (c config) standaloneDatabase(snowCtx *snow.Context) (_ database.Database, retErr error) {
	path := filepath.Join(c.DatabasePath, c.DatabaseType)
	snowCtx.Log.Info("using standalone database",
		zap.String("type", c.DatabaseType),
		zap.String("path", path),
	)

	dbReg, err := metrics.MakeAndRegister(snowCtx.Metrics, "db")
	if err != nil {
		return nil, fmt.Errorf("registering db metrics: %w", err)
	}
	const readOnly = false
	db, err := factory.New(c.DatabaseType, path, readOnly, c.DatabaseConfig, dbReg, snowCtx.Log)
	if err != nil {
		return nil, fmt.Errorf("creating db: %w", err)
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, db.Close())
		}
	}()

	meterdbReg, err := metrics.MakeAndRegister(snowCtx.Metrics, "meterdb")
	if err != nil {
		return nil, fmt.Errorf("registering meterdb metrics: %w", err)
	}
	meterDB, err := meterdb.New(meterdbReg, db)
	if err != nil {
		return nil, fmt.Errorf("creating meterdb: %w", err)
	}
	return meterDB, nil
}
