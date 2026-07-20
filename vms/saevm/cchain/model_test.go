// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"math/big"
	"sync"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/holiman/uint256"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
)

// TestModel drives randomized action sequences against a real cchain VM and
// checks the model + invariants after every action. Reproduce failures with
// -rapid.seed / -rapid.failfile; explore more deeply with e.g.
// go test -tags test -run TestModel -rapid.checks=5000 ./vms/saevm/cchain/
func TestModel(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		cfg := genRunConfig().Draw(rt, "runConfig")
		mm := newModelMachine(t, rt, cfg)
		defer mm.tb.close()
		rt.Repeat(mm.actions())
	})
}

type txKind int

const (
	kindTransfer txKind = iota
	kindDeploy
	kindStore
	kindRevert
	kindWarpSend
	kindWarpReceive
)

// warpLoggerAddr is a genesis account whose code forwards its calldata to the
// warp precompile and logs the returned data, letting issueWarpReceive
// observe GetVerifiedWarpMessage(Output) via a receipt log.
var warpLoggerAddr = common.Address{'m', 'o', 'd', 'e', 'l', '-', 'w', 'a', 'r', 'p'}

// atomicKeyGenesisBalanceWei is the genesis eth-side balance (in wei) funded
// to each atomic account, both in the model (as a *uint256.Int) and in the
// SUT's genesis alloc (as a *big.Int) via baseOptions.
const atomicKeyGenesisBalanceWei = "1000000000000000000000" // 1000 ETH-equivalent

// issuedTx carries the model's expectations for a tx issued to the pool but
// not yet observed in an accepted block.
type issuedTx struct {
	kind  txKind
	from  common.Address
	to    common.Address
	value *uint256.Int
	cost  *uint256.Int // worst-case pool cost: value + gasLimit*feeCap

	contract   common.Address // kindDeploy (predicted CREATE address), kindStore/kindRevert (target)
	key, val   common.Hash    // kindStore
	deployKind txKind         // kindDeploy: kindStore or kindRevert
	wantStatus uint64         // kindRevert: expected receipt status

	payload     []byte // kindWarpSend: the message payload to send
	wantLogData []byte // kindWarpReceive: expected forwardAndLogCode log data
}

// contractState is the model's expectation for one deployed fixture.
type contractState struct {
	kind    txKind // kindStore or kindRevert
	storage map[common.Hash]common.Hash
}

// provisionedUTXO is a shared-memory UTXO the harness wrote as the remote
// chain, spendable by atomic key ownerIdx via an import.
type provisionedUTXO struct {
	utxo     *avax.UTXO
	ownerIdx int
	amount   uint64 // nAVAX
}

// atomicExpectation predicts the effects of an issued cross-chain tx once its
// block is accepted.
type atomicExpectation struct {
	isImport    bool
	senderIdx   int            // atomic key index
	to          common.Address // import credit target
	creditWei   *uint256.Int   // import: ScaleAVAX(imported - fee)
	debitWei    *uint256.Int   // export: ScaleAVAX(amount + fee)
	remoteChain ids.ID
	consumed    []*provisionedUTXO // import: removed from shared memory; restored on restart if dropped
	exported    []*avax.UTXO       // export: appear in shared memory
}

// utxosOf projects the raw UTXOs out of provisioned entries, for the
// shared-memory assertion helpers.
func utxosOf(ps []*provisionedUTXO) []*avax.UTXO {
	out := make([]*avax.UTXO, len(ps))
	for i, p := range ps {
		out[i] = p.utxo
	}
	return out
}

// model is the lightweight predicted state of the VM. Gas costs are
// reconciled from receipts (not predicted); everything else is exact.
type model struct {
	balances    map[common.Address]*uint256.Int
	nonces      map[common.Address]uint64
	pendingEth  map[common.Hash]*issuedTx
	pendingCost map[common.Address]*uint256.Int
	contracts   map[common.Address]*contractState

	pendingAtomic  map[ids.ID]*atomicExpectation
	availableUTXOs map[ids.ID][]*provisionedUTXO
	exportedUTXOs  map[ids.ID][]*avax.UTXO
	consumedUTXOs  map[ids.ID][]*avax.UTXO

	target        dynamic.TargetExponent
	price         dynamic.PriceExponent
	delay         dynamic.DelayExponent
	desiredTarget *dynamic.TargetExponent
	desiredPrice  *dynamic.PriceExponent
	desiredDelay  *dynamic.DelayExponent

	lastAccepted       *blocks.Block // nil until the run's first block
	lastAcceptedID     ids.ID
	lastAcceptedHeight uint64
	lastGasTime        *gastime.Time
}

// modelCore is the SUT-agnostic heart shared by the single-node and networked
// machines: the predicted model, the shared wallet, and issuance/apply/check
// logic parameterized by the target SUT. Methods that touch a VM take the
// target (ctx, sut) explicitly so the networked machine can aim them at any
// node.
type modelCore struct {
	tb     *scopedTB
	m      *model
	wallet *saetest.Wallet
	addrs  []common.Address

	pendingEthTxs []*types.Transaction
}

type modelMachine struct {
	*modelCore
	cfg runConfig

	ctx   context.Context
	sut   *SUT
	clock *saetest.Clock

	vdrs *warptest.Validators

	atomicKeys    []*secp256k1.PrivateKey
	atomicWallets []*wallet
	atomicAddrs   []common.Address

	db      database.Database
	dbDir   string
	dataDir string
	timeOpt sutOption
}

const txGasFeeCap = 100 // wei; comfortably above the ~1 wei base fee a short run can reach

func newModelMachine(t *testing.T, rt *rapid.T, cfg runConfig) *modelMachine {
	tb := newScopedTB(t)
	tb.setRapidT(rt)

	keys := saetest.NewUNSAFEKeyChain(tb, cfg.numAccounts)
	timeOpt, clock := withVMTime(testStartTime)

	mm := &modelMachine{
		modelCore: &modelCore{
			tb:     tb,
			wallet: saetest.NewWalletWithKeyChain(keys, types.LatestSigner(saetest.ChainConfig())),
			addrs:  keys.Addresses(),
		},
		cfg:     cfg,
		clock:   clock,
		vdrs:    warptest.NewValidators(tb, cfg.numValidators),
		dataDir: t.TempDir(),
		timeOpt: timeOpt,
	}

	switch cfg.kv {
	case kvLevelDB:
		mm.dbDir = t.TempDir()
		db, err := leveldb.New(mm.dbDir, nil, logging.NoLog{}, prometheus.NewRegistry())
		require.NoErrorf(tb, err, "leveldb.New(%q)", mm.dbDir)
		mm.db = db
		tb.Cleanup(func() { _ = mm.db.Close() })
	default:
		mm.db = memdb.New()
	}

	m := &model{
		balances:       make(map[common.Address]*uint256.Int),
		nonces:         make(map[common.Address]uint64),
		pendingEth:     make(map[common.Hash]*issuedTx),
		pendingCost:    make(map[common.Address]*uint256.Int),
		contracts:      make(map[common.Address]*contractState),
		pendingAtomic:  make(map[ids.ID]*atomicExpectation),
		availableUTXOs: make(map[ids.ID][]*provisionedUTXO),
		exportedUTXOs:  make(map[ids.ID][]*avax.UTXO),
		consumedUTXOs:  make(map[ids.ID][]*avax.UTXO),
		target:         dynamic.InitialTargetExponent,
		price:          dynamic.InitialPriceExponent,
		delay:          dynamic.InitialDelayExponent,
	}
	for i, addr := range mm.addrs {
		bal, overflow := uint256.FromBig(cfg.balance(i))
		require.Falsef(tb, overflow, "genesis balance of account %d overflows uint256", i)
		m.balances[addr] = bal
		m.pendingCost[addr] = new(uint256.Int)
	}
	for range cfg.numAtomicKeys {
		sk := txtest.NewKey(tb)
		mm.atomicKeys = append(mm.atomicKeys, sk)
		mm.atomicAddrs = append(mm.atomicAddrs, sk.EthAddress())
		m.balances[sk.EthAddress()] = uint256.MustFromDecimal(atomicKeyGenesisBalanceWei)
		m.pendingCost[sk.EthAddress()] = new(uint256.Int)
	}
	if cfg.gasTarget != nil {
		d := dynamic.DesiredTargetExponent(*cfg.gasTarget)
		m.desiredTarget = &d
	}
	if cfg.priceTarget != nil {
		d := dynamic.DesiredPriceExponent(*cfg.priceTarget)
		m.desiredPrice = &d
	}
	if cfg.minDelayMS != nil {
		d := dynamic.DesiredDelayExponent(*cfg.minDelayMS)
		m.desiredDelay = &d
	}
	mm.modelCore.m = m

	mm.openSUT()
	return mm
}

// baseOptions returns every sutOption except withDB, so restarts can reuse
// the persisted database while re-deriving everything else identically.
func (mm *modelMachine) baseOptions() []sutOption {
	opts := []sutOption{
		mm.timeOpt,
		withValidators(mm.vdrs),
		mm.cfg.storageOptions(),
		withChainDataDir(mm.dataDir),
		// The saexec queue is sized 2*CommitInterval (saexec.go) but restart
		// recovery enqueues the full unsettled backlog (recovery.go), whose
		// bound is settlement lag, not CommitInterval; the overflow WARN is
		// benign (Enqueue blocks; no loss). Tolerated pending an upstream
		// fix — see task-9 analysis.
		withToleratedLogMessage("Execution queue buffer full"),
	}
	for i, addr := range mm.addrs {
		opts = append(opts, withAccount(addr, types.Account{Balance: mm.cfg.balance(i)}))
	}
	for _, addr := range mm.atomicAddrs {
		bal, ok := new(big.Int).SetString(atomicKeyGenesisBalanceWei, 10)
		require.Truef(mm.tb, ok, "big.Int.SetString(%q, 10)", atomicKeyGenesisBalanceWei)
		opts = append(opts, withAccount(addr, types.Account{Balance: bal}))
	}
	opts = append(opts, withAccount(warpLoggerAddr, types.Account{
		Code: forwardAndLogCode(mm.tb, corethwarp.ContractAddress),
	}))
	if mm.cfg.gasTarget != nil {
		opts = append(opts, withGasTarget(*mm.cfg.gasTarget))
	}
	if mm.cfg.priceTarget != nil {
		opts = append(opts, withPriceTarget(*mm.cfg.priceTarget))
	}
	if mm.cfg.minDelayMS != nil {
		opts = append(opts, withMinDelayTarget(*mm.cfg.minDelayMS))
	}
	return opts
}

func (mm *modelMachine) openSUT() {
	ctx, sut := newSUT(mm.tb, append(mm.baseOptions(), withDB(mm.db))...)
	mm.ctx = ctx
	mm.sut = sut
	if mm.m.lastAccepted == nil {
		id, err := sut.LastAccepted(ctx)
		require.NoErrorf(mm.tb, err, "%T.LastAccepted()", sut.VM)
		mm.m.lastAcceptedID = id
	}

	// Atomic wallets hold the SUT's Client, so they must be (re)created every
	// time the SUT is (re)opened; restore each wallet's nonce from the model
	// so a restart doesn't replay already-applied export nonces.
	mm.atomicWallets = mm.atomicWallets[:0]
	for _, sk := range mm.atomicKeys {
		w := newWallet(sk, mm.sut.ctx, mm.sut.Client)
		w.nonce = mm.m.nonces[sk.EthAddress()]
		mm.atomicWallets = append(mm.atomicWallets, w)
	}
}

// scopedTB adapts the outer *testing.T for use inside a rapid property check.
// It scopes Cleanup callbacks to a single check (run via close) and routes
// failure methods through the active *rapid.T so rapid can shrink failing
// action sequences. All other testing.TB behaviour (Context, TempDir, Logf,
// Helper, ...) delegates to the embedded outer TB.
type scopedTB struct {
	testing.TB

	mu       sync.Mutex
	rt       *rapid.T
	cleanups []func()
}

func newScopedTB(t *testing.T) *scopedTB { return &scopedTB{TB: t} }

func (s *scopedTB) setRapidT(rt *rapid.T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rt = rt
}

func (s *scopedTB) Cleanup(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanups = append(s.cleanups, f)
}

// close runs and clears the scoped cleanups in reverse registration order,
// mirroring testing.T cleanup semantics for a single rapid check.
func (s *scopedTB) close() {
	s.mu.Lock()
	fns := s.cleanups
	s.cleanups = nil
	s.mu.Unlock()
	for i := len(fns) - 1; i >= 0; i-- {
		fns[i]()
	}
}

func (s *scopedTB) rapidT() *rapid.T {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rt
}

func (s *scopedTB) Error(args ...any) {
	if rt := s.rapidT(); rt != nil {
		rt.Error(args...)
		return
	}
	s.TB.Error(args...)
}

func (s *scopedTB) Errorf(format string, args ...any) {
	if rt := s.rapidT(); rt != nil {
		rt.Errorf(format, args...)
		return
	}
	s.TB.Errorf(format, args...)
}

func (s *scopedTB) Fatal(args ...any) {
	if rt := s.rapidT(); rt != nil {
		rt.Fatal(args...)
		return
	}
	s.TB.Fatal(args...)
}

func (s *scopedTB) Fatalf(format string, args ...any) {
	if rt := s.rapidT(); rt != nil {
		rt.Fatalf(format, args...)
		return
	}
	s.TB.Fatalf(format, args...)
}

func (s *scopedTB) Fail() {
	if rt := s.rapidT(); rt != nil {
		rt.Fail()
		return
	}
	s.TB.Fail()
}

func (s *scopedTB) FailNow() {
	if rt := s.rapidT(); rt != nil {
		rt.FailNow()
		return
	}
	s.TB.FailNow()
}

func TestScopedTBCleanupOrder(t *testing.T) {
	s := newScopedTB(t)
	var got []int
	s.Cleanup(func() { got = append(got, 1) })
	s.Cleanup(func() { got = append(got, 2) })
	s.close()
	require.Equal(t, []int{2, 1}, got, "scopedTB.close() cleanup order")
	s.close() // idempotent
	require.Equal(t, []int{2, 1}, got, "scopedTB.close() second call is a no-op")
}
