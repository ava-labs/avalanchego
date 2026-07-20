// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
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
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
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
)

// issuedTx carries the model's expectations for a tx issued to the pool but
// not yet observed in an accepted block.
type issuedTx struct {
	kind  txKind
	from  common.Address
	to    common.Address
	value *uint256.Int
	cost  *uint256.Int // worst-case pool cost: value + gasLimit*feeCap
}

// model is the lightweight predicted state of the VM. Gas costs are
// reconciled from receipts (not predicted); everything else is exact.
type model struct {
	balances    map[common.Address]*uint256.Int
	nonces      map[common.Address]uint64
	pendingEth  map[common.Hash]*issuedTx
	pendingCost map[common.Address]*uint256.Int

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

type modelMachine struct {
	tb  *scopedTB
	cfg runConfig

	ctx   context.Context
	sut   *SUT
	clock *saetest.Clock

	wallet *saetest.Wallet
	addrs  []common.Address
	vdrs   *warptest.Validators

	db      database.Database
	dbDir   string
	dataDir string
	timeOpt sutOption

	m             *model
	pendingEthTxs []*types.Transaction
}

const txGasFeeCap = 100 // wei; comfortably above the ~1 wei base fee a short run can reach

func newModelMachine(t *testing.T, rt *rapid.T, cfg runConfig) *modelMachine {
	tb := newScopedTB(t)
	tb.setRapidT(rt)

	keys := saetest.NewUNSAFEKeyChain(tb, cfg.numAccounts)
	timeOpt, clock := withVMTime(testStartTime)

	mm := &modelMachine{
		tb:      tb,
		cfg:     cfg,
		clock:   clock,
		wallet:  saetest.NewWalletWithKeyChain(keys, types.LatestSigner(saetest.ChainConfig())),
		addrs:   keys.Addresses(),
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
		balances:    make(map[common.Address]*uint256.Int),
		nonces:      make(map[common.Address]uint64),
		pendingEth:  make(map[common.Hash]*issuedTx),
		pendingCost: make(map[common.Address]*uint256.Int),
		target:      dynamic.InitialTargetExponent,
		price:       dynamic.InitialPriceExponent,
		delay:       dynamic.InitialDelayExponent,
	}
	for i, addr := range mm.addrs {
		bal, overflow := uint256.FromBig(cfg.balance(i))
		require.Falsef(tb, overflow, "genesis balance of account %d overflows uint256", i)
		m.balances[addr] = bal
		m.pendingCost[addr] = new(uint256.Int)
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
	mm.m = m

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
		// TODO(Task 9): withChainDataDir(mm.dataDir) once it exists.
	}
	for i, addr := range mm.addrs {
		opts = append(opts, withAccount(addr, types.Account{Balance: mm.cfg.balance(i)}))
	}
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
