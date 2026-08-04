// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package corethgen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/eth/tracers"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
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
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/corethtest"
)

var update = flag.Bool("update", false, "regenerate the committed fixture")

func TestMain(m *testing.M) {
	evm.RegisterAllLibEVMExtras()
	os.Exit(m.Run())
}

// fixturePath locates the committed fixture, which lives with the
// [corethtest] package that consumers import rather than with this
// generator, relative to this package's directory.
const fixturePath = "../../../../../vms/saevm/cchain/corethtest/fixture.json"

// TestFixtureUpToDate regenerates the fixture from scratch and requires that
// it carries the same content as the committed one. Under `go test -update`
// (see the [update] flag) it instead overwrites the committed fixture.
func TestFixtureUpToDate(t *testing.T) {
	fx := generate(t)
	got, err := json.MarshalIndent(fx, "", "\t")
	require.NoError(t, err, "json.MarshalIndent(fixture)")

	if *update {
		// .editorconfig mandates a final newline in committed files.
		require.NoError(t, os.WriteFile(fixturePath, append(got, '\n'), 0o644), "os.WriteFile(%s)", fixturePath)
		return
	}

	// Both sides are re-encoded from a [corethtest.Fixture] so that the
	// comparison sees content alone, leaving the committed file's formatting to
	// the write branch above.
	want, err := json.MarshalIndent(corethtest.Load(t), "", "\t")
	require.NoError(t, err, "json.MarshalIndent(committed fixture)")
	require.True(t, bytes.Equal(want, got),
		"committed fixture is stale; run `go generate ./plugin/evm/corethgen` and inspect the diff")
}

// forkSchedule returns the fixture's upgrade config. No upgrade is initially
// active, so the chain starts under the launch rules. Upgrades are then a day
// apart, dwarfing the block intervals, so adding blocks to an era never crosses
// into the next. Caveat: AP2/AP3 activate block-number forks (Berlin/London)
// whose heights are pinned per chain ID ([params.TestUpgradechainChainID]) and
// must be updated when a block is added to the launch, AP1 or AP2 era.
func forkSchedule() upgrade.Config {
	cfg := upgradetest.GetConfig(upgradetest.NoUpgrades)
	at := func(days int) time.Time {
		return upgrade.InitiallyActiveTime.Add(time.Duration(days) * 24 * time.Hour)
	}
	cfg.ApricotPhase1Time = at(1)
	cfg.ApricotPhase2Time = at(2)
	cfg.ApricotPhase3Time = at(3)
	cfg.ApricotPhase4Time = at(4)
	cfg.ApricotPhase5Time = at(5)
	cfg.ApricotPhasePre6Time = at(6)
	cfg.ApricotPhase6Time = at(7)
	cfg.ApricotPhasePost6Time = at(8)
	cfg.BanffTime = at(9)
	cfg.CortinaTime = at(10)
	cfg.DurangoTime = at(11)
	cfg.EtnaTime = at(12)
	cfg.FortunaTime = at(13)
	cfg.GraniteTime = at(14)
	return cfg
}

// Deterministic identifiers used throughout the fixture. The BLS keys are the
// scalars 1 and 2; weak, but valid and reproducible.
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

	fixture *corethtest.Fixture
}

const minValidPChainHeight = 10

var errPChainHeightTooLow = errors.New("warp validator set unavailable below the minimum P-chain height")

// generate builds the full fixture: chain, blocks, and database dump.
func generate(t *testing.T) *corethtest.Fixture {
	upgrades := forkSchedule()

	// The fixture's dedicated chain ID selects the pinned Berlin and London
	// activation heights that let the chain cross AP2 and AP3 mid-chain.
	genesis := vmtest.NewTestGenesis(paramstest.ForkToChainConfig[upgradetest.NoUpgrades])
	genesis.Config.ChainID = params.TestUpgradechainChainID
	genesisBytes, err := json.Marshal(genesis)
	require.NoError(t, err, "json.Marshal(genesis)")
	genesisJSON := string(genesisBytes)

	g := &generator{
		vm: vm.WrapVM(&evm.VM{}),
		kc: secp256k1fx.NewKeychain(vmtest.TestKeys...),
		// Fixed BLS keys so the embedded signed warp message, and hence the
		// fixture, is deterministic.
		warpValidators: warptest.NewValidators(t, warptest.WithSigners(blsSigner(t, 1), blsSigner(t, 2))),
		fixture: &corethtest.Fixture{
			Genesis:  json.RawMessage(genesisJSON),
			Upgrades: upgrades,
		},
	}

	g.setClock(upgrade.InitiallyActiveTime)
	suite := vmtest.SetupTestVM(t, g.vm, vmtest.TestVMConfig{
		Upgrades:    &upgrades,
		GenesisJSON: genesisJSON,
		ConfigJSON:  vmConfigJSON(t),
	})
	g.ctx = suite.Ctx
	g.memory = suite.AtomicMemory
	g.configureValidatorState(t)

	g.recordGenesis(t)
	g.buildAllBlocks(t)
	g.pinNativeAssetCallTraceParity(t)
	g.recordRPCCalls(t)

	// The dump MUST follow a clean shutdown, matching a real handed-over
	// database and removing geth's unclean-shutdown marker, whose wall-clock
	// content would make the dump nondeterministic.
	require.NoError(t, g.vm.Shutdown(t.Context()), "vm.Shutdown()")
	g.dumpDatabase(t, suite)
	return g.fixture
}

// vmConfigJSON returns the generating VM's configuration.
//
// Pruning is disabled so every block's state root remains resolvable, as is
// snapshot generation, whose async writes would make the database dump racy.
// The enabled APIs are exactly [recordedRPCNamespaces], because coreth
// attaches only the services it is asked for and rejects unknown names.
func vmConfigJSON(t *testing.T) string {
	t.Helper()

	apis, err := json.Marshal(recordedRPCNamespaces)
	require.NoError(t, err, "json.Marshal(recordedRPCNamespaces)")
	return fmt.Sprintf(
		`{"pruning-enabled": false, "snapshot-cache": 0, "eth-apis": %s}`,
		apis,
	)
}

// pinNativeAssetCallTraceParity requires that coreth itself fails to callTrace
// each of [NativeAssetCallBlocks] with exactly [NativeAssetCallTraceError].
//
// The SAE tests reproduce this error as proof of trace parity with coreth,
// but only the generator has a live coreth VM to confirm it's what coreth
// actually does. If coreth ever fixes or rewords the failure we find out
// here, at regeneration.
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

	g.fixture.Database = make(map[string]hexutil.Bytes)
	it := suite.DB.NewIterator()
	defer it.Release()
	for it.Next() {
		g.fixture.Database[hexutil.Encode(it.Key())] = bytes.Clone(it.Value())
	}
	require.NoError(t, it.Error(), "iterating VM database")
}

// setClock sets the VM's mocked clock, which drives block timestamps and
// fork-rule selection.
func (g *generator) setClock(now time.Time) {
	g.vm.Clock().Set(now)
}

// watchedAddresses returns the accounts whose state the recorded RPC calls
// query, in a fixed order. The blackhole coinbase is watched because coreth
// burns fees by crediting it, a write that replaying consumers MUST reproduce
// to reach the recorded roots.
func (g *generator) watchedAddresses() []common.Address {
	return []common.Address{
		vmtest.TestEthAddrs[0],
		vmtest.TestEthAddrs[1],
		transferRecipient,
		g.counter,
		evmconstants.BlackholeAddr,
	}
}
