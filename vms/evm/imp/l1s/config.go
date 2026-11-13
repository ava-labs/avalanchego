// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/ava-labs/libevm/common"

	_ "github.com/ava-labs/avalanchego/database/leveldb" // For [leveldb.Name] comment resolution
	_ "github.com/ava-labs/avalanchego/database/memdb"   // For [memdb.Name] comment resolution

	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blackhole"
	"github.com/ava-labs/avalanchego/vms/evm/imp/saeconfig"
	"github.com/ava-labs/avalanchego/vms/evm/sae"
	"github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
)

// config is the operator-supplied per-chain config for the L1 VM.
//
// TODO: Support [saeconfig.Config.StateSyncEnabled] and
// [saeconfig.Config.PriceTarget], which are accepted but ignored.
type config struct {
	saeconfig.Config

	// FeeRecipient is paid the fees of blocks built by this node, if the chain
	// allows custom fee recipients.
	FeeRecipient common.Address `json:"fee-recipient"`

	// UseStandaloneDatabase, if true, stores chain data in a database opened by
	// the VM instead of the node's database. It is ignored if the node's
	// database already holds the chain.
	UseStandaloneDatabase bool `json:"use-standalone-database"`
	// DatabaseConfig is the JSON config of the standalone database.
	DatabaseConfig json.RawMessage `json:"database-config"`
	// DatabasePath is the parent directory of the standalone database, which is
	// stored in DatabasePath/DatabaseType.
	DatabasePath string `json:"database-path"`
	// DatabaseType is [leveldb.Name], [memdb.Name] or [pebbledb.Name].
	DatabaseType string `json:"database-type"`

	deprecatedConfig
}

// validatorAPI is the [rpc.API] of the `validator` namespace, which is served
// by the L1 VM.
const validatorAPI rpc.API = "validator"

func defaultConfig(chainDataDir string) config {
	c := config{
		Config:                saeconfig.Default(),
		FeeRecipient:          blackhole.Address,
		UseStandaloneDatabase: true,
		DatabasePath:          filepath.Join(chainDataDir, "db"),
		DatabaseType:          pebbledb.Name,
	}
	c.APIs.Add(validatorAPI)
	return c
}

// parseConfig unmarshals operator-supplied per-chain config bytes on top of
// [defaultConfig], warning about unrecognized options.
func parseConfig(snowCtx *snow.Context, b []byte) (config, error) {
	c := defaultConfig(snowCtx.ChainDataDir)
	if err := saeconfig.Parse(snowCtx.Log, b, &c); err != nil {
		return config{}, err
	}
	// TODO: Remove deprecated config handling after Igloo is activated.
	if err := c.applyDeprecated(snowCtx.Log, b); err != nil {
		return config{}, err
	}
	if err := c.sae().Verify(snowCtx.NetworkID); err != nil {
		return config{}, err
	}
	return c, nil
}

// sae returns the [saeconfig.Config] embedded in c, without the [rpc.API]s
// served by the L1 VM.
func (c config) sae() saeconfig.Config {
	s := c.Config
	s.APIs = set.NewSet[rpc.API](c.APIs.Len())
	for api := range c.APIs {
		if api != validatorAPI {
			s.APIs.Add(api)
		}
	}
	return s
}

// saeConfig translates the operator-supplied [config] into the [sae.Config]
// consumed by [sae.NewVM].
func (c config) saeConfig(now func() time.Time) sae.Config {
	return c.sae().SAEConfig(now)
}
