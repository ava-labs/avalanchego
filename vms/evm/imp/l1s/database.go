// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database/factory"
	"github.com/ava-labs/avalanchego/database/meterdb"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/evm/types"

	apimetrics "github.com/ava-labs/avalanchego/api/metrics"
	avadb "github.com/ava-labs/avalanchego/database"
)

// useStandaloneDatabase reports whether chain data is stored in the database
// returned by [config.standaloneDatabase] rather than in avaDB.
func (c config) useStandaloneDatabase(networkID uint32, avaDB avadb.Database) bool {
	if c.UseStandaloneDatabase != nil {
		return *c.UseStandaloneDatabase
	}
	// Unit tests inspect the database they provide.
	if networkID == constants.UnitTestID {
		return false
	}
	// A chain already in avaDB stays there.
	return rawdb.ReadCanonicalHash(types.NewChainEthDB(avaDB), genesisNumber) == (common.Hash{})
}

// standaloneDatabase opens the database configured by c, registering its
// metrics with snowCtx. The caller MUST close the returned database.
func (c config) standaloneDatabase(snowCtx *snow.Context) (avadb.Database, error) {
	dbConfig, err := c.databaseConfig()
	if err != nil {
		return nil, err
	}

	dir := c.DatabasePath
	if dir == "" {
		dir = filepath.Join(snowCtx.ChainDataDir, "db")
	}
	path := filepath.Join(dir, c.DatabaseType)
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
	var (
		b   []byte
		err error
	)
	switch {
	case c.DatabaseConfigContent != "":
		b, err = base64.StdEncoding.DecodeString(c.DatabaseConfigContent)
		if err != nil {
			return nil, fmt.Errorf("decoding base64 database config: %w", err)
		}
	case c.DatabaseConfigFile != "":
		b, err = os.ReadFile(c.DatabaseConfigFile)
		if err != nil {
			return nil, fmt.Errorf("reading database config file: %w", err)
		}
	}
	if c.DatabaseType != pebbledb.Name {
		return b, nil
	}

	cfg := pebbledb.DefaultConfig
	// Sync mode in pebble has an issue with OSs like MacOS.
	cfg.Sync = false
	if len(b) > 0 {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return nil, fmt.Errorf("json.Unmarshal(%T): %w", cfg, err)
		}
	}
	return json.Marshal(cfg)
}
