// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pchain

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/rpc/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/genesis"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/block/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
)

// fundedKeyBalance is the P-Chain genesis balance of each funded key. The
// genesistest default cannot cover one validator's minimum stake. Keep it far
// below the maximum supply so staking rewards stay realistic.
const fundedKeyBalance = 100 * units.KiloAvax

// VM is one PlatformVM with a real database behind its RPC handler, so
// tests can use the public client and wallet unchanged. Tests issue
// transactions and query state through Node, and use the methods to create
// lifecycle and time events that a real network would produce.
type VM struct {
	Node Node

	tb testing.TB

	lifecycleLock sync.RWMutex
	// acceptLock is held by PauseAcceptance to keep the accept loop from
	// building blocks.
	acceptLock sync.Mutex
	chainCtx   *snow.Context
	chainTime  time.Time
	db         database.Database
	genesis    []byte
	vm         *platformvm.VM
	handler    http.Handler
	stop       func()
}

// NewVM starts a VM with local network parameters and every upgrade active. It
// funds each key at genesis and accepts a block whenever it has pending
// transactions. Cleanups registered on tb stop the server and the VM when the
// calling test returns.
func NewVM(tb testing.TB, fundedKeys []*secp256k1.PrivateKey) *VM {
	// Start chain time after genesis so dynamic fee capacity has refilled.
	genesisTime := genesistest.DefaultValidatorStartTime
	v := &VM{
		tb:        tb,
		chainTime: genesisTime.Add(time.Hour),
		db:        memdb.New(),
		genesis: genesistest.NewBytes(tb, genesistest.Config{
			FundedKeys:         fundedKeys,
			InitialBalance:     fundedKeyBalance,
			ValidatorWeight:    genesis.LocalParams.MinValidatorStake,
			ValidatorStartTime: genesisTime,
			ValidatorEndTime:   genesisTime.Add(genesistest.DefaultValidatorDuration),
		}),
	}
	v.start()

	mux := http.NewServeMux()
	mux.Handle("/ext/P", v)
	mux.Handle("/ext/info", newInfoHandler(tb, v.chainCtx.NetworkID))
	server := httptest.NewServer(mux)
	// Close the server first so no request reaches a VM that is shutting down.
	tb.Cleanup(func() {
		server.Close()
		v.stop()
	})
	v.Node = NewNode(ids.EmptyNodeID, server.URL)
	return v
}

func (v *VM) start() {
	v.chainCtx = snowtest.Context(v.tb, snowtest.PChainID)
	// Shared memory is stored in the database, so it persists across Reopen.
	v.chainCtx.SharedMemory = atomic.NewMemory(prefixdb.New([]byte{1}, v.db)).NewSharedMemory(v.chainCtx.ChainID)

	params := genesis.LocalParams
	vm := &platformvm.VM{Internal: config.Internal{
		Chains:                  chains.TestManager,
		Validators:              validators.NewManager(),
		UptimeLockedCalculator:  uptime.NewLockedCalculator(),
		SybilProtectionEnabled:  true,
		DynamicFeeConfig:        params.DynamicFeeConfig,
		ValidatorFeeConfig:      params.ValidatorFeeConfig,
		UptimePercentage:        params.UptimeRequirement,
		MinValidatorStake:       params.MinValidatorStake,
		MaxValidatorStake:       params.MaxValidatorStake,
		MinDelegatorStake:       params.MinDelegatorStake,
		MinDelegationFee:        params.MinDelegationFee,
		MinStakeDuration:        params.MinStakeDuration,
		MaxStakeDuration:        params.MaxStakeDuration,
		HeliconMinStakeDuration: params.HeliconMinStakeDuration,
		RewardConfig:            params.RewardConfig,
		UpgradeConfig:           upgradetest.GetConfig(upgradetest.Latest),
	}}

	// An unset gossip sender returns an error, which would fail transaction
	// gossip.
	appSender := &enginetest.Sender{}
	appSender.SendAppGossipF = func(context.Context, common.SendConfig, []byte) error {
		return nil
	}

	vm.Clock().Set(v.chainTime)

	// Unlock on assertion failure so cleanup cannot deadlock.
	func() {
		v.chainCtx.Lock.Lock()
		defer v.chainCtx.Lock.Unlock()

		require.NoError(v.tb, vm.Initialize(
			v.tb.Context(),
			v.chainCtx,
			prefixdb.New([]byte{0}, v.db),
			v.genesis,
			nil,
			nil,
			nil,
			appSender,
		))
		require.NoError(v.tb, vm.SetState(v.tb.Context(), snow.NormalOp))
		// Rewards require uptime, which accrues only for connected validators.
		for _, nodeID := range genesistest.DefaultNodeIDs {
			require.NoError(v.tb, vm.Connected(v.tb.Context(), nodeID, version.Current))
		}
	}()

	handlers, err := vm.CreateHandlers(v.tb.Context())
	require.NoError(v.tb, err)
	v.vm = vm
	v.handler = handlers[""]

	acceptCtx, cancelAccept := context.WithCancel(v.tb.Context())
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			if _, err := vm.WaitForEvent(acceptCtx); err != nil {
				return
			}
			v.acceptLock.Lock()
			err := v.acceptBlock(acceptCtx)
			v.acceptLock.Unlock()
			if err != nil && !errors.Is(err, builder.ErrNoPendingBlocks) {
				if acceptCtx.Err() != nil || errors.Is(err, context.Canceled) {
					return
				}
				v.tb.Errorf("failed to accept block: %v", err)
				return
			}
		}
	}()

	v.stop = func() {
		cancelAccept()
		<-accepted
		v.chainCtx.Lock.Lock()
		defer v.chainCtx.Lock.Unlock()
		require.NoError(v.tb, vm.Shutdown(v.tb.Context()))
	}
}

// Reopen replaces the VM with a new instance on the same database and
// endpoint, so a test can verify persisted state through the public API.
func (v *VM) Reopen() {
	v.lifecycleLock.Lock()
	defer v.lifecycleLock.Unlock()

	stop := v.stop
	v.stop = func() {}
	stop()
	v.start()
}

// PauseAcceptance keeps pending transactions out of blocks until the returned
// function is called, so a test can observe them while they are processing.
// Resume before Reopen, or Reopen hangs waiting for the paused accept loop.
// AdvanceTime ignores the pause and builds pending transactions into blocks.
func (v *VM) PauseAcceptance() (resume func()) {
	v.acceptLock.Lock()
	return v.acceptLock.Unlock
}

// ChainTime returns the time configured on the current VM.
func (v *VM) ChainTime() time.Time {
	v.lifecycleLock.RLock()
	defer v.lifecycleLock.RUnlock()

	return v.chainTime
}

// AdvanceTime sets chain time and accepts every block that becomes due,
// including the system transactions a real network would produce.
func (v *VM) AdvanceTime(to time.Time) {
	v.lifecycleLock.Lock()
	defer v.lifecycleLock.Unlock()

	// The VM reads its clock under the chain lock.
	v.chainCtx.Lock.Lock()
	v.vm.Clock().Set(to)
	v.chainCtx.Lock.Unlock()
	v.chainTime = to

	for {
		err := v.acceptBlock(v.tb.Context())
		if errors.Is(err, builder.ErrNoPendingBlocks) {
			return
		}
		require.NoError(v.tb, err)
	}
}

// ServeHTTP routes to the current VM's RPC handler.
func (v *VM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	v.lifecycleLock.RLock()
	defer v.lifecycleLock.RUnlock()

	v.handler.ServeHTTP(w, r)
}

// acceptBlock builds and accepts one block, and for a proposal its preferred
// option as well.
func (v *VM) acceptBlock(ctx context.Context) error {
	v.chainCtx.Lock.Lock()
	defer v.chainCtx.Lock.Unlock()

	blk, err := v.vm.BuildBlock(ctx)
	if err != nil {
		return err
	}
	if err := blk.Verify(ctx); err != nil {
		return err
	}
	options, err := blk.(snowman.OracleBlock).Options(ctx)
	isProposal := err == nil
	if !isProposal && !errors.Is(err, snowman.ErrNotOracle) {
		return err
	}

	if err := blk.Accept(ctx); err != nil {
		return err
	}
	if !isProposal {
		return v.vm.SetPreference(ctx, blk.ID())
	}

	preferred := options[0]
	if err := preferred.Verify(ctx); err != nil {
		return err
	}
	if err := preferred.Accept(ctx); err != nil {
		return err
	}
	return v.vm.SetPreference(ctx, preferred.ID())
}

// infoService answers the one info API call the wallet makes while syncing.
type infoService struct {
	networkID uint32
}

func (s *infoService) GetNetworkID(_ *http.Request, _ *struct{}, reply *info.GetNetworkIDReply) error {
	reply.NetworkID = json.Uint32(s.networkID)
	return nil
}

func newInfoHandler(tb testing.TB, networkID uint32) http.Handler {
	server := rpc.NewServer()
	server.RegisterCodec(json.NewCodec(), "application/json")
	server.RegisterCodec(json.NewCodec(), "application/json;charset=UTF-8")
	require.NoError(tb, server.RegisterService(&infoService{networkID: networkID}, "info"))
	return server
}
