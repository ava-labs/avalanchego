// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgradechain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/eth/tracers"
	"github.com/ava-labs/avalanchego/graft/coreth/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/vm"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/avalanchego/graft/evm/rpc"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	evmconstants "github.com/ava-labs/avalanchego/graft/evm/constants"
)

var update = flag.Bool("update", false, "regenerate fixture.json")

func TestMain(m *testing.M) {
	evm.RegisterAllLibEVMExtras()
	os.Exit(m.Run())
}

// TestFixtureUpToDate regenerates the fixture from scratch and requires that
// it matches the committed fixture.json byte for byte.
func TestFixtureUpToDate(t *testing.T) {
	fx := generate(t)
	got, err := json.MarshalIndent(fx, "", "\t")
	require.NoError(t, err, "json.MarshalIndent(fixture)")
	got = append(got, '\n')

	if *update {
		require.NoError(t, os.WriteFile("fixture.json", got, 0o644), "os.WriteFile(fixture.json)")
		return
	}
	require.True(t, bytes.Equal(fixtureJSON, got),
		"committed fixture.json is stale or the generator is nondeterministic; run `go generate ./plugin/evm/upgradechain` and inspect the diff")
}

// forkSchedule returns the upgrade config the fixture chain is generated
// with. AP1-AP3 (and hence Berlin and London) are active at genesis; every
// later scheduled upgrade activates one day after its predecessor.
func forkSchedule() upgrade.Config {
	cfg := upgradetest.GetConfig(upgradetest.ApricotPhase3)
	at := func(days int) time.Time {
		return upgrade.InitiallyActiveTime.Add(time.Duration(days) * 24 * time.Hour)
	}
	cfg.ApricotPhase4Time = at(1)
	cfg.ApricotPhase5Time = at(2)
	cfg.ApricotPhasePre6Time = at(3)
	cfg.ApricotPhase6Time = at(4)
	cfg.ApricotPhasePost6Time = at(5)
	cfg.BanffTime = at(6)
	cfg.CortinaTime = at(7)
	cfg.DurangoTime = at(8)
	cfg.EtnaTime = at(9)
	cfg.FortunaTime = at(10)
	cfg.GraniteTime = at(11)
	return cfg
}

// Deterministic identifiers used throughout the fixture. The BLS keys are the
// scalars 1 and 2; weak, but valid and reproducible.
var (
	warpSourceChainID = ids.ID{'w', 'a', 'r', 'p', '-', 's', 'o', 'u', 'r', 'c', 'e'}
	antAssetID        = ids.ID{'a', 'n', 't', '-', 'a', 's', 's', 'e', 't'}
	transferRecipient = common.Address{0xde, 0xad}

	// counterCreationCode deploys a 26-byte contract: called with empty
	// call data it increments its storage slot 0; called with any call data
	// it returns the slot's value as a 32-byte word.
	//	CALLDATASIZE; PUSH1 0x0e; JUMPI
	//	PUSH1 0; SLOAD; PUSH1 1; ADD; PUSH1 0; SSTORE; STOP
	//	JUMPDEST; PUSH1 0; SLOAD; PUSH1 0; MSTORE; PUSH1 32; PUSH1 0; RETURN
	counterCreationCode = common.Hex2Bytes("7936600e57600054600101600055005b60005460005260206000f3600052601a6006f3")
)

func blsSigner(t *testing.T, scalar byte) *localsigner.LocalSigner {
	skBytes := make([]byte, 32)
	skBytes[31] = scalar
	sk, err := localsigner.FromBytes(skBytes)
	require.NoError(t, err, "localsigner.FromBytes(scalar=%d)", scalar)
	return sk
}

type generator struct {
	vm    *vm.VM
	ctx   *snow.Context
	clock time.Time // mirror of the VM's mocked clock

	memory         *atomic.Memory
	kc             *secp256k1fx.Keychain
	warpValidators *warptest.Validators

	counter common.Address // the counter contract, deployed in block 1

	utxoTxID uint64 // distinct txIDs for seeded shared-memory UTXOs
	ethNonce uint64 // next nonce for the single EVM sender

	fixture *Fixture
}

const minValidPChainHeight = 10

var errPChainHeightTooLow = errors.New("warp validator set unavailable below the minimum P-chain height")

// generate builds the full fixture: chain, blocks, and database dump.
func generate(t *testing.T) *Fixture {
	fork := upgradetest.ApricotPhase3
	upgrades := forkSchedule()
	genesisJSON := vmtest.GenesisJSON(paramstest.ForkToChainConfig[fork])

	counter := crypto.CreateAddress(vmtest.TestEthAddrs[0], 0)
	g := &generator{
		vm: vm.WrapVM(&evm.VM{}),
		kc: secp256k1fx.NewKeychain(vmtest.TestKeys...),
		// Fixed BLS keys so the embedded signed warp message, and hence the
		// fixture, is deterministic.
		warpValidators: warptest.NewValidatorsWithSigners(blsSigner(t, 1), blsSigner(t, 2)),
		counter:        counter,
		fixture: &Fixture{
			Genesis:    json.RawMessage(genesisJSON),
			Upgrades:   upgrades,
			Counter:    counter,
			ANTAssetID: antAssetID,
		},
	}

	g.setClock(t, upgrade.InitiallyActiveTime)
	suite := vmtest.SetupTestVM(t, g.vm, vmtest.TestVMConfig{
		Fork:        &fork,
		Upgrades:    &upgrades,
		GenesisJSON: genesisJSON,
		// Disable pruning so every block's state root remains resolvable, and
		// snapshot generation, whose async writes would make the dump racy.
		ConfigJSON: `{"pruning-enabled": false, "snapshot-cache": 0}`,
	})
	g.ctx = suite.Ctx
	g.memory = suite.AtomicMemory
	g.configureValidatorState(t)

	g.buildAllBlocks(t)
	g.pinNativeAssetCallTraceParity(t)
	g.dumpDatabase(t, suite)
	return g.fixture
}

// pinNativeAssetCallTraceParity requires that coreth itself fails to callTrace
// each of [NativeAssetCallBlocks] with exactly [NativeAssetCallTraceError].
func (g *generator) pinNativeAssetCallTraceParity(t *testing.T) {
	t.Helper()

	api := tracers.NewAPI(g.vm.Ethereum().APIBackend)
	tracer := "callTracer"
	config := &tracers.TraceConfig{Tracer: &tracer}
	for _, number := range NativeAssetCallBlocks {
		_, err := api.TraceBlockByNumber(t.Context(), rpc.BlockNumber(number), config) //#nosec G115 -- fixture heights are small
		require.EqualError(t, err, NativeAssetCallTraceError, "coreth TraceBlockByNumber(%d, callTracer)", number)
	}
}

func (g *generator) configureValidatorState(t *testing.T) {
	t.Helper()

	vdrState, ok := g.ctx.ValidatorState.(*validatorstest.State)
	require.True(t, ok, "unexpected type %T for validator state", g.ctx.ValidatorState)
	vdrState.T = t
	vdrState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return minValidPChainHeight, nil
	}
	// Resolve every chain — including the fictional warp source chain — to
	// the primary network so the warp predicate finds a validator set.
	vdrState.GetSubnetIDF = func(context.Context, ids.ID) (ids.ID, error) {
		return constants.PrimaryNetworkID, nil
	}
	vdrState.GetWarpValidatorSetsF = func(_ context.Context, height uint64) (map[ids.ID]validators.WarpSet, error) {
		if height < minValidPChainHeight {
			return nil, errPChainHeightTooLow
		}
		return map[ids.ID]validators.WarpSet{
			constants.PrimaryNetworkID: g.warpValidators.WarpSet(),
		}, nil
	}
}

func (g *generator) dumpDatabase(t *testing.T, suite *vmtest.TestVMSuite) {
	t.Helper()

	// The dump MUST follow a clean shutdown: that is the state of a real
	// handed-over database, and shutdown removes geth's unclean-shutdown
	// marker, whose wall-clock content would make the dump nondeterministic.
	// Only the VM's own database wrappers close, so the suite's separate
	// handle remains iterable.
	require.NoError(t, g.vm.Shutdown(t.Context()), "vm.Shutdown()")
	it := suite.DB.NewIterator()
	defer it.Release()
	for it.Next() {
		g.fixture.Database = append(g.fixture.Database, KV{
			Key:   bytes.Clone(it.Key()),
			Value: bytes.Clone(it.Value()),
		})
	}
	require.NoError(t, it.Error(), "iterating VM database")
}

// setClock sets the VM's mocked clock, which drives block timestamps and
// fork-rule selection.
func (g *generator) setClock(t *testing.T, now time.Time) {
	t.Helper()
	g.clock = now
	g.vm.Clock().Set(now)
}

// watchedState reads the watched accounts' balances and nonces from statedb.
// The blackhole coinbase is watched because coreth "burns" transaction fees by
// crediting it — a state write replay-based consumers MUST reproduce to arrive
// at the recorded state roots.
func (g *generator) watchedState(statedb *state.StateDB) map[common.Address]AccountState {
	multicoin := extstate.New(statedb)
	accounts := make(map[common.Address]AccountState)
	for _, addr := range []common.Address{
		vmtest.TestEthAddrs[0],
		vmtest.TestEthAddrs[1],
		transferRecipient,
		g.counter,
		evmconstants.BlackholeAddr,
	} {
		accounts[addr] = AccountState{
			Balance:    (*hexutil.Big)(statedb.GetBalance(addr).ToBig()),
			Nonce:      statedb.GetNonce(addr),
			ANTBalance: (*hexutil.Big)(multicoin.GetBalanceMultiCoin(addr, common.Hash(antAssetID))),
		}
	}
	return accounts
}
