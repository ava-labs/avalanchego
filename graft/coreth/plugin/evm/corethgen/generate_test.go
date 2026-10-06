// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package corethgen generates SAE's synchronoustest fixture.
package corethgen

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/vm"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/synchronoustest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	evmconstants "github.com/ava-labs/avalanchego/graft/evm/constants"
	commoneng "github.com/ava-labs/avalanchego/snow/engine/common"
)

//go:generate go test -run TestFixtureUpToDate -update .

var update = flag.Bool("update", false, "regenerate the committed fixture")

func TestMain(m *testing.M) {
	evm.RegisterAllLibEVMExtras()
	os.Exit(m.Run())
}

// fixturePath locates the committed fixture, which lives with the
// [synchronoustest] package that consumers import rather than with this
// generator, relative to this package's directory.
const fixturePath = "../../../../../vms/saevm/cchain/synchronoustest/fixture.json"

// TestFixtureUpToDate regenerates the fixture from scratch and requires that
// it carries the same content as the committed one. Under `go test -update`
// (see the [update] flag) it instead overwrites the committed fixture.
func TestFixtureUpToDate(t *testing.T) {
	fx := generate(t)
	got, err := json.MarshalIndent(fx, "", "\t")
	require.NoError(t, err, "json.MarshalIndent(fixture)")

	if *update {
		require.NoError(t, os.WriteFile(fixturePath, got, 0o644), "os.WriteFile(%s)", fixturePath)
		return
	}

	// Both sides are re-encoded from a [synchronoustest.Fixture] so that the
	// comparison sees content alone, leaving the committed file's formatting to
	// the write branch above.
	want, err := json.Marshal(synchronoustest.Load(t))
	require.NoError(t, err, "json.MarshalIndent(committed fixture)")
	require.JSONEq(t, string(want), string(got), "committed fixture is stale")
}

var (
	warpSourceChainID = ids.ID{'w', 'a', 'r', 'p', '-', 's', 'o', 'u', 'r', 'c', 'e'}
	antAssetID        = ids.ID{'a', 'n', 't', '-', 'a', 's', 's', 'e', 't'}
	transferRecipient = common.Address{0xde, 0xad}
)

func blsSigner(t *testing.T, scalar byte) *localsigner.LocalSigner {
	skBytes := make([]byte, 32)
	skBytes[31] = scalar
	sk, err := localsigner.FromBytes(skBytes)
	require.NoError(t, err, "localsigner.FromBytes(scalar=%d)", scalar)
	return sk
}

type generator struct {
	vm  *vm.VM
	ctx *snow.Context

	memory         *atomic.Memory
	kc             *secp256k1fx.Keychain
	warpValidators *warptest.Validators

	// counter is the address of the counter contract, set by
	// [generator.counterDeployTx]. With empty call data it increments its
	// storage slot 0; with any call data it returns the slot's value as a
	// 32-byte word.
	counter common.Address

	utxoTxID uint64 // distinct txIDs for seeded shared-memory UTXOs
	ethNonce uint64 // next nonce for the single EVM sender

	fixture *synchronoustest.Fixture
}

// generate builds the full fixture: chain, blocks, and database dump.
func generate(t *testing.T) *synchronoustest.Fixture {
	// The fixture's dedicated chain ID selects the pinned Berlin and London
	// activation heights that let the chain cross AP2 and AP3 mid-chain.
	genesis := vmtest.NewTestGenesis(paramstest.ForkToChainConfig[upgradetest.NoUpgrades])
	genesis.Config.ChainID = params.TestFixtureChainID
	genesisJSON, err := json.Marshal(genesis)
	require.NoError(t, err, "json.Marshal(genesis)")

	// upgrades is an upgrade config that schedules a network upgrade each day
	// up through the Granite upgrade.
	upgrades := upgradetest.GetConfig(upgradetest.NoUpgrades)
	for u := upgradetest.Granite; u > upgradetest.NoUpgrades; u-- {
		d := time.Duration(u) * 24 * time.Hour
		upgradetest.SetTimesTo(&upgrades, u, upgrade.InitiallyActiveTime.Add(d))
	}

	g := &generator{
		vm: vm.WrapVM(&evm.VM{}),
		kc: secp256k1fx.NewKeychain(vmtest.TestKeys...),
		// Fixed BLS keys so the embedded signed warp message is deterministic.
		warpValidators: warptest.NewValidators(t, warptest.WithSigners(
			blsSigner(t, 1),
			blsSigner(t, 2),
		)),
		fixture: &synchronoustest.Fixture{
			Genesis:  json.RawMessage(genesisJSON),
			Upgrades: upgrades,
		},
	}

	g.setClock(upgrade.InitiallyActiveTime)
	// Shared memory commits the VM's batches atomically by unwrapping them to
	// the base database, so both MUST share one.
	baseDB := memdb.New()
	sharedMemoryDB := prefixdb.New([]byte{0}, baseDB)
	db := prefixdb.New([]byte{1}, baseDB)
	g.initialize(t, db, sharedMemoryDB, genesisJSON, upgrades)
	g.configureValidatorState(t)

	g.recordGenesisBlock(t)
	g.buildAllBlocks(t)
	g.recordRPCCalls(t)

	// The dump MUST follow a clean shutdown, matching a real handed-over
	// database and removing geth's unclean-shutdown marker.
	require.NoError(t, g.vm.Shutdown(t.Context()), "vm.Shutdown()")
	g.fixture.Database = dump(t, db)
	g.fixture.SharedMemory = dump(t, sharedMemoryDB)
	return g.fixture
}

// initialize initializes the VM as [vmtest.SetupTestVM] does, but with shared
// memory backed by sharedMemoryDB so that the fixture can record it.
func (g *generator) initialize(t *testing.T, db, sharedMemoryDB database.Database, genesisJSON []byte, upgrades upgrade.Config) {
	t.Helper()

	g.ctx = snowtest.Context(t, snowtest.CChainID)
	g.ctx.NetworkUpgrades = upgrades
	g.memory = atomic.NewMemory(sharedMemoryDB)
	g.ctx.SharedMemory = g.memory.NewSharedMemory(g.ctx.ChainID)
	// Matching [vmtest.SetupGenesis], the lock is intentionally left locked.
	g.ctx.Lock.Lock()

	appSender := &enginetest.Sender{
		T:                 t,
		CantSendAppGossip: true,
		SendAppGossipF:    func(context.Context, commoneng.SendConfig, []byte) error { return nil },
	}
	const config = `{
		"pruning-enabled": false,
		"snapshot-cache": 0,
		"eth-apis": [
			"internal-blockchain",
			"internal-transaction",
			"eth-filter",
			"debug-tracer"
		]
	}`

	ctx := t.Context()
	require.NoError(t, g.vm.Initialize(ctx, g.ctx, db, genesisJSON, nil, []byte(config), nil, appSender), "vm.Initialize()")
	require.NoError(t, g.vm.SetState(ctx, snow.Bootstrapping), "vm.SetState(Bootstrapping)")
	require.NoError(t, g.vm.SetState(ctx, snow.NormalOp), "vm.SetState(NormalOp)")
}

func (g *generator) configureValidatorState(t *testing.T) {
	t.Helper()

	vdrState, ok := g.ctx.ValidatorState.(*validatorstest.State)
	require.True(t, ok, "unexpected type %T for validator state", g.ctx.ValidatorState)
	vdrState.T = t
	vdrState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return 0, nil
	}
	vdrState.GetSubnetIDF = func(context.Context, ids.ID) (ids.ID, error) {
		return constants.PrimaryNetworkID, nil
	}
	vdrState.GetWarpValidatorSetsF = func(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
		return map[ids.ID]validators.WarpSet{
			constants.PrimaryNetworkID: g.warpValidators.WarpSet(),
		}, nil
	}
}

// dump returns every key-value pair of db.
func dump(t *testing.T, db database.Iteratee) map[string]hexutil.Bytes {
	t.Helper()

	entries := make(map[string]hexutil.Bytes)
	it := db.NewIterator()
	defer it.Release()
	for it.Next() {
		entries[hexutil.Encode(it.Key())] = bytes.Clone(it.Value())
	}
	require.NoError(t, it.Error(), "iterating database")
	return entries
}

// setClock sets the VM's clock, which drives block timestamps and fork-rule
// selection.
func (g *generator) setClock(now time.Time) {
	g.vm.Clock().Set(now)
}

// watchedAddresses returns the accounts whose state the recorded RPC calls
// query, in a fixed order.
func (g *generator) watchedAddresses() []common.Address {
	return []common.Address{
		vmtest.TestEthAddrs[0],
		vmtest.TestEthAddrs[1],
		transferRecipient,
		g.counter,
		evmconstants.BlackholeAddr,
	}
}
