// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database/factory"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/types"

	apimetrics "github.com/ava-labs/avalanchego/api/metrics"
	avadb "github.com/ava-labs/avalanchego/database"
)

// useStandaloneDatabase reports whether chain data is stored in the database
// returned by [config.standaloneDatabase] rather than in avaDB. A chain already
// in avaDB stays there, regardless of [config.UseStandaloneDatabase].
func (c config) useStandaloneDatabase(log logging.Logger, avaDB avadb.Database) bool {
	if rawdb.ReadCanonicalHash(types.NewChainEthDB(avaDB), genesisNumber) == (common.Hash{}) {
		return c.UseStandaloneDatabase
	}
	if c.UseStandaloneDatabase {
		log.Warn(`ignoring "use-standalone-database" because the node's database already holds the chain`)
	}
	return false
}

// standaloneDatabase opens the database configured by c, registering its
// metrics with snowCtx. The caller MUST close the returned database.
func (c config) standaloneDatabase(snowCtx *snow.Context) (avadb.Database, error) {
	dbConfig, err := c.databaseConfig()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(c.DatabasePath, c.DatabaseType)
	snowCtx.Log.Info("using standalone database",
		zap.String("type", c.DatabaseType),
		zap.String("path", path),
	)

	reg, err := apimetrics.MakeAndRegister(snowCtx.Metrics, "db")
	if err != nil {
		return nil, err
	}
	db, err := factory.New(c.DatabaseType, path, false, dbConfig, reg, snowCtx.Log)
	if err != nil {
		return nil, err
	}

	meterReg, err := apimetrics.MakeAndRegister(snowCtx.Metrics, "meterdb")
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	meterDB, err := meterdb.New(meterReg, db)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("creating meterdb: %w", err), db.Close())
	}
	return meterDB, nil
}

// databaseConfig returns the JSON config of the standalone database.
func (c config) databaseConfig() ([]byte, error) {
	if c.DatabaseType != pebbledb.Name {
		return c.DatabaseConfig, nil
	}

	cfg := pebbledb.DefaultConfig
	// Sync mode in pebble has an issue with OSs like MacOS.
	cfg.Sync = false
	if len(c.DatabaseConfig) > 0 {
		if err := json.Unmarshal(c.DatabaseConfig, &cfg); err != nil {
			return nil, fmt.Errorf("json.Unmarshal(%T): %w", cfg, err)
		}
	}
	return json.Marshal(cfg)
}
