// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saevm

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/strevm/sae"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils"

	avadb "github.com/ava-labs/avalanchego/database"
	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
)

// SinceGenesis is a harness around an [sae.VM], providing an `Initialize`
// method that treats the chain as being asynchronous since genesis.
type SinceGenesis struct {
	*sae.VM // created by [SinceGenesis.Initialize]

	config sae.Config

	lastWaitForEvent utils.Atomic[time.Time]
}

// NewSinceGenesis constructs a new [SinceGenesis].
func NewSinceGenesis(c sae.Config) *SinceGenesis {
	return &SinceGenesis{config: c}
}

// Initialize initializes the VM.
func (vm *SinceGenesis) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	avaDB avadb.Database,
	genesisBytes []byte,
	_ []byte,
	_ []byte,
	_ []*common.Fx,
	appSender common.AppSender,
) error {
	db := rawdb.NewDatabase(evmdb.New(avaDB))
	tdb := triedb.NewDatabase(db, vm.config.TrieDBConfig)

	genesis := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, genesis); err != nil {
		return fmt.Errorf("json.Unmarshal(%T): %w", genesis, err)
	}

	{
		c := genesis.Config
		u := snowCtx.NetworkUpgrades

		c.HomesteadBlock = big.NewInt(0)
		c.DAOForkBlock = big.NewInt(0)
		c.DAOForkSupport = true
		c.EIP150Block = big.NewInt(0)
		c.EIP155Block = big.NewInt(0)
		c.EIP158Block = big.NewInt(0)
		c.ByzantiumBlock = big.NewInt(0)
		c.ConstantinopleBlock = big.NewInt(0)
		c.PetersburgBlock = big.NewInt(0)
		c.IstanbulBlock = big.NewInt(0)
		c.MuirGlacierBlock = big.NewInt(0)
		c.BerlinBlock = big.NewInt(0)
		c.LondonBlock = big.NewInt(0)
		c.ShanghaiTime = utils.PointerTo(uint64(u.DurangoTime.Unix()))
		c.CancunTime = utils.PointerTo(uint64(u.EtnaTime.Unix()))
	}

	config, _, err := core.SetupGenesisBlock(db, tdb, genesis)
	if err != nil {
		return fmt.Errorf("core.SetupGenesisBlock(...): %w", err)
	}

	inner, err := sae.NewVM(ctx, vm.config, snowCtx, config, db, genesis.ToBlock(), appSender)
	if err != nil {
		return err
	}
	vm.VM = inner
	return nil
}

// Prevent busy looping when the chain is more advanced than the mempool.
const waitForEventDelay = 100 * time.Millisecond

// WaitForEvent waits for the next event from the VM.
func (vm *SinceGenesis) WaitForEvent(ctx context.Context) (common.Message, error) {
	defer func() {
		vm.lastWaitForEvent.Set(time.Now())
	}()

	sinceLastCall := time.Since(vm.lastWaitForEvent.Get())
	timeToWait := waitForEventDelay - sinceLastCall
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(timeToWait):
		return vm.VM.WaitForEvent(ctx)
	}
}

// Shutdown gracefully closes the VM.
func (vm *SinceGenesis) Shutdown(ctx context.Context) error {
	if vm.VM == nil {
		return nil
	}
	return vm.VM.Shutdown(ctx)
}
