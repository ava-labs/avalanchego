// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package eth

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/graft/evm/rpc"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/eth/ethconfig"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/eth/tracers"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/feemanager"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	_ "github.com/ava-labs/libevm/eth/tracers/native" // registers callTracer
	ethparams "github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	core.RegisterExtras()
	customtypes.Register()
	params.RegisterExtras()
	os.Exit(m.Run())
}

// TestFirewoodReconstructedStateLeak is a memory-leak harness for the Firewood
// historical-state read path (firewoodReconstructedState, used by debug_trace*
// and eth_call at historical heights).
//
// It builds an archive (pruning disabled) Firewood chain with a small commit
// interval, picks a block whose root is neither in the in-memory revision
// window nor persisted, and then reconstructs the state at that block many
// times. Between measurements it forces a Go GC and returns freed Go memory to
// the OS, so any remaining RSS growth is outside the Go heap (Rust/ffi side).
//
// Knobs (environment):
//
//	FW_LEAK_ITERS      total reconstructions (default 2000)
//	FW_LEAK_PAR        parallel workers (default 1)
//	FW_LEAK_FEEMANAGER "0" disables the FeeManager precompile; with it enabled
//	                   (default) stock master fails every call with
//	                   "engine finalization check failed: no revision found".
//	FW_LEAK_BLOCKS     chain length (default 512)
//	FW_LEAK_COMMIT     commit interval (default 64)
//	FW_LEAK_API        "1" goes through the debug_trace* API (callTracer on the
//	                   block after the target, and traceTransaction on its first
//	                   tx) instead of calling stateAtBlock directly.
//
// The Rust allocator (jemalloc, prefixed build) honours _RJEM_MALLOC_CONF, so
// its page purging can be tuned per run, e.g.
// _RJEM_MALLOC_CONF=background_thread:true,dirty_decay_ms:1000,muzzy_decay_ms:1000
//
// Run:
//
//	FW_LEAK_ITERS=2000 FW_LEAK_PAR=200 FW_LEAK_BLOCKS=1400 FW_LEAK_COMMIT=512 FW_LEAK_FEEMANAGER=0 \
//	  go test ./graft/subnet-evm/eth/ -run TestFirewoodReconstructedStateLeak -v -count=1
func TestFirewoodReconstructedStateLeak(t *testing.T) {
	if os.Getenv("FW_LEAK_ITERS") == "" && testing.Short() {
		t.Skip("set FW_LEAK_ITERS to run the leak harness")
	}
	iters := envInt("FW_LEAK_ITERS", 2000)
	par := envInt("FW_LEAK_PAR", 1)
	nBlocks := envInt("FW_LEAK_BLOCKS", 512)
	commitInterval := envInt("FW_LEAK_COMMIT", 64)
	feeManager := os.Getenv("FW_LEAK_FEEMANAGER") != "0"
	useAPI := os.Getenv("FW_LEAK_API") == "1"
	// Keep the in-memory window small so most historical roots are only
	// reachable through replay from a persisted checkpoint, as on a freshly
	// restarted production node.
	stateHistory := commitInterval + 32

	key, err := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	require.NoError(t, err)
	addr := crypto.PubkeyToAddress(key.PublicKey)

	config := params.Copy(params.TestChainConfig)
	if feeManager {
		params.GetExtra(&config).GenesisPrecompiles = extras.Precompiles{
			feemanager.ConfigKey: feemanager.NewConfig(utils.PointerTo[uint64](0), []common.Address{addr}, nil, nil, nil),
		}
	}
	gspec := &core.Genesis{
		Config: &config,
		Alloc:  types.GenesisAlloc{addr: {Balance: new(big.Int).Mul(big.NewInt(1_000_000), big.NewInt(ethparams.Ether))}},
	}

	engine := dummy.NewFakerWithMode(dummy.Mode{ModeSkipBlockFee: true, ModeSkipCoinbase: true})
	signer := types.LatestSigner(&config)
	var nonce uint64
	// Each block touches a few fresh accounts so every replayed block writes
	// new trie nodes into the reconstructed view.
	_, blocks, _, err := core.GenerateChainWithGenesis(gspec, engine, nBlocks, 10, func(i int, b *core.BlockGen) {
		for j := 0; j < 8; j++ {
			var to common.Address
			binaryPut(to[:], uint64(i)*8+uint64(j)+1)
			tx, err := types.SignTx(types.NewTransaction(nonce, to, big.NewInt(1), ethparams.TxGas, b.BaseFee(), nil), signer, key)
			require.NoError(t, err)
			b.AddTx(tx)
			nonce++
		}
	})
	require.NoError(t, err)

	db := rawdb.NewMemoryDatabase()
	cacheConfig := &core.CacheConfig{
		TrieCleanLimit:            256,
		TrieDirtyLimit:            256,
		TriePrefetcherParallelism: 4,
		SnapshotLimit:             0,
		Pruning:                   false,
		CommitInterval:            uint64(commitInterval),
		StateScheme:               customrawdb.FirewoodScheme,
		StateHistory:              uint64(stateHistory),
		ChainDataDir:              t.TempDir(),
	}
	chain, err := core.NewBlockChain(db, cacheConfig, gspec, engine, vm.Config{}, common.Hash{}, false)
	require.NoError(t, err)
	defer chain.Stop()
	_, err = chain.InsertChain(blocks)
	require.NoError(t, err)
	for _, b := range blocks {
		require.NoError(t, chain.Accept(b))
	}
	chain.DrainAcceptorQueue()

	// Find the historical block with the longest replay distance: outside the
	// in-memory window and as far as possible above its nearest persisted root.
	var (
		target       *types.Block
		lastPersist  uint64
		bestDistance uint64
	)
	for n := uint64(0); n+uint64(stateHistory)+1 < uint64(nBlocks); n++ {
		b := chain.GetBlockByNumber(n)
		if chain.HasState(b.Root()) {
			lastPersist = n
			continue
		}
		if d := n - lastPersist; d > bestDistance {
			bestDistance, target = d, b
		}
	}
	require.NotNil(t, target, "no non-persisted historical block found")
	t.Logf("chain=%d blocks commitInterval=%d stateHistory=%d feeManager=%v", nBlocks, commitInterval, stateHistory, feeManager)
	t.Logf("target block %d, replay distance %d blocks from nearest persisted root", target.NumberU64(), bestDistance)

	e := &Ethereum{
		config:     &ethconfig.Config{Genesis: gspec, RPCGasCap: 50_000_000},
		blockchain: chain,
		chainDb:    db,
		engine:     engine,
	}
	e.APIBackend = &EthAPIBackend{eth: e}
	api := tracers.NewAPI(e.APIBackend)
	tracerName := "callTracer"
	reexec := uint64(nBlocks)
	traceCfg := &tracers.TraceConfig{Tracer: &tracerName, Reexec: &reexec}
	// Tracing the block after the target needs the state at the target.
	traced := chain.GetBlockByNumber(target.NumberU64() + 1)
	require.NotEmpty(t, traced.Transactions())
	_ = ffi.StartMetrics() // may already be started; the gatherer still works

	var (
		okCount, errCount atomic.Int64
		firstErr          sync.Once
	)
	var callN atomic.Int64
	call := func() {
		var err error
		switch {
		case !useAPI:
			var release tracers.StateReleaseFunc
			_, release, err = e.stateAtBlock(context.Background(), target, reexec, nil, true, false)
			if err == nil {
				release()
			}
		case callN.Add(1)%2 == 0:
			_, err = api.TraceBlockByNumber(context.Background(), rpc.BlockNumber(traced.NumberU64()), traceCfg)
		default:
			_, err = api.TraceTransaction(context.Background(), traced.Transactions()[0].Hash(), traceCfg)
		}
		if err != nil {
			errCount.Add(1)
			firstErr.Do(func() { t.Logf("call error (reported once): %v", err) })
			return
		}
		okCount.Add(1)
	}

	step := max(iters/10, 1)
	t.Logf("%8s %8s %10s %10s %10s %10s %10s %10s %10s %10s %10s", "iter", "ok", "err", "rssPreGC", "heapInuse", "heapSys", "goSys", "rss", "jeResident", "jeAlloc", "jeRetained")
	t.Log(memRow(0, &okCount, &errCount))
	for done := 0; done < iters; done += step {
		n := min(step, iters-done)
		var wg sync.WaitGroup
		perWorker := n / par
		for w := 0; w < par; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < perWorker; i++ {
					call()
				}
			}()
		}
		wg.Wait()
		t.Log(memRow(done+perWorker*par, &okCount, &errCount))
	}
	// Idle: shows whether the Rust allocator returns freed pages to the OS
	// once the burst is over (production: jemalloc resident stays at the
	// high-water mark while jemalloc allocated is tiny).
	time.Sleep(15 * time.Second)
	t.Log(memRow(iters, &okCount, &errCount) + "  (after 15s idle)")
}

func binaryPut(dst []byte, v uint64) {
	for i := 0; i < 8; i++ {
		dst[len(dst)-1-i] = byte(v >> (8 * i))
	}
}

func envInt(name string, def int) int {
	if s := os.Getenv(name); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil {
			panic(fmt.Sprintf("%s=%q: %v", name, s, err))
		}
		return v
	}
	return def
}

// memRow reports process RSS as seen without intervention (rssPreGC), then
// forces a full GC (so ffi cleanups run and dead Go objects are gone), returns
// freed spans to the OS, and reports Go heap, process RSS and the Rust
// allocator's resident bytes. All values in MiB. RSS growth that survives the
// forced GC is not reclaimable Go garbage.
func memRow(iter int, ok, errs *atomic.Int64) string {
	pre := vmRSS()
	runtime.GC()
	debug.FreeOSMemory()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	mib := func(b uint64) float64 { return float64(b) / (1 << 20) }
	je := jemalloc()
	return fmt.Sprintf("%8d %8d %10d %10.1f %10.1f %10.1f %10.1f %10.1f %10.1f %10.1f %10.1f",
		iter, ok.Load(), errs.Load(), mib(pre), mib(ms.HeapInuse), mib(ms.HeapSys), mib(ms.Sys), mib(vmRSS()),
		mib(je["jemalloc_resident_bytes"]), mib(je["jemalloc_allocated_bytes"]), mib(je["jemalloc_retained_bytes"]))
}

func vmRSS() uint64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			f := strings.Fields(line)
			kb, _ := strconv.ParseUint(f[1], 10, 64)
			return kb * 1024
		}
	}
	return 0
}

// jemalloc returns the Rust allocator gauges exported by the Firewood ffi
// (jemalloc_resident_bytes, jemalloc_allocated_bytes, jemalloc_retained_bytes, ...).
func jemalloc() map[string]uint64 {
	out := map[string]uint64{}
	fams, err := ffi.GatherRenderedMetrics()
	if err != nil {
		return out
	}
	for _, f := range fams {
		if strings.HasPrefix(f.GetName(), "jemalloc_") && len(f.Metric) > 0 && f.Metric[0].Gauge != nil {
			out[f.GetName()] = uint64(f.Metric[0].Gauge.GetValue())
		}
	}
	return out
}
