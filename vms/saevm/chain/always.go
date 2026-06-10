package chain

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/statesync"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"
	"github.com/prometheus/client_golang/prometheus"
)

func newEthDB(db database.Database) ethdb.Database {
	return rawdb.NewDatabase(evmdb.New(db))
}

var _ adaptor.SyncableVM[*blocks.Block, *statesync.Summary] = (*SinceGenesis[hook.Transaction])(nil)

// SinceGenesis is a harness around a [VM], providing an `Initialize` method that
// treats the chain as being asynchronous since genesis.
type SinceGenesis[T hook.Transaction] struct {
	*VM[*statesync.Summary] // created by [SinceGenesis.Initialize]

	hooks  hook.PointsG[T]
	config sae.Config
}

// NewSinceGenesis constructs a new [SinceGenesis].
func NewSinceGenesis[T hook.Transaction](hooks hook.PointsG[T], c sae.Config) *SinceGenesis[T] {
	return &SinceGenesis[T]{
		hooks:  hooks,
		config: c,
	}
}

// Initialize implements [adaptor.SyncableVM].
func (vm *SinceGenesis[_]) Initialize(
	ctx context.Context,
	snowCtx *snow.Context,
	avaDB database.Database,
	genesisBytes []byte,
	configBytes []byte,
	upgradeBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	db := newEthDB(avaDB)
	tdb := triedb.NewDatabase(db, vm.config.DBConfig.TrieDBConfig)

	genesis := new(core.Genesis)
	if err := json.Unmarshal(genesisBytes, genesis); err != nil {
		return fmt.Errorf("json.Unmarshal(%T): %v", genesis, err)
	}
	config, _, err := core.SetupGenesisBlock(db, tdb, genesis)
	if err != nil {
		return fmt.Errorf("core.SetupGenesisBlock(...): %v", err)
	}

	metrics := prometheus.NewRegistry()
	n, err := sae.NewNetwork(snowCtx, appSender, metrics)
	if err != nil {
		return fmt.Errorf("newNetwork(...): %v", err)
	}

	syncable := statesync.NewSummaryHandler(statesync.Config{}, db, n, snowCtx)

	// The chain [VM] is constructed lazily: [sae.NewVM] is only called once the
	// VM transitions out of state syncing (see [VM.SetState]).
	lazyChain := func() (adaptor.Chain[*blocks.Block], error) {
		inner, err := sae.NewVM(ctx, vm.hooks, vm.config, snowCtx, config, db, genesis.ToBlock(), metrics, n)
		if err != nil {
			return nil, fmt.Errorf("sae.NewVM(...): %v", err)
		}
		return inner, nil
	}

	inner, err := NewVM(n, syncable, lazyChain)
	if err != nil {
		return err
	}
	vm.VM = inner
	return nil
}
