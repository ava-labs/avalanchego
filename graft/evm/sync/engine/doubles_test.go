// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package engine

import (
	"context"
	"errors"
	"math"
	"math/big"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/graft/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"

	ethtypes "github.com/ava-labs/libevm/core/types"
)

// mockEthBlockWrapper implements [EthBlockWrapper] for testing.
type mockEthBlockWrapper struct {
	ethBlock  *ethtypes.Block
	acceptErr error
	rejectErr error
	verifyErr error

	acceptCount int
	rejectCount int
	verifyCount int
}

func newMockBlock(height uint64) *mockEthBlockWrapper {
	header := &ethtypes.Header{Number: new(big.Int).SetUint64(height)}
	return &mockEthBlockWrapper{
		ethBlock: ethtypes.NewBlockWithHeader(header),
	}
}

func (m *mockEthBlockWrapper) GetEthBlock() *ethtypes.Block { return m.ethBlock }
func (m *mockEthBlockWrapper) Accept(context.Context) error {
	m.acceptCount++
	return m.acceptErr
}

func (m *mockEthBlockWrapper) Reject(context.Context) error {
	m.rejectCount++
	return m.rejectErr
}

func (m *mockEthBlockWrapper) Verify(context.Context) error {
	m.verifyCount++
	return m.verifyErr
}

var _ EthBlockWrapper = (*mockEthBlockWrapper)(nil)

// FuncSyncer adapts a function to the simple Syncer shape used in tests. It is
// useful for defining small, behavior-driven syncers inline. When targetHeight
// is set, the syncer also implements [types.TargetReporter].
type FuncSyncer struct {
	name         string
	fn           func(ctx context.Context) error
	targetHeight *uint64
}

// Sync calls the wrapped function and returns its result.
func (f FuncSyncer) Sync(ctx context.Context) error { return f.fn(ctx) }
func (f FuncSyncer) Name() string                   { return f.name }
func (f FuncSyncer) ID() string                     { return f.name }
func (FuncSyncer) UpdateTarget(message.Syncable) error {
	return nil
}

// TargetHeight returns the configured target height, or math.MaxUint64 if none
// was set (meaning no block preservation constraint).
func (f FuncSyncer) TargetHeight() uint64 {
	if f.targetHeight != nil {
		return *f.targetHeight
	}
	return math.MaxUint64
}

var (
	_ types.Syncer         = FuncSyncer{}
	_ types.TargetReporter = FuncSyncer{}
)

func NewTargetReporterSyncer(name string, height uint64) FuncSyncer {
	return FuncSyncer{name: name, targetHeight: &height}
}

// NewBarrierSyncer returns a syncer that signals startedWG.Done() when Sync begins,
// then blocks until releaseCh is closed (returns nil) or ctx is canceled (returns ctx.Err).
func NewBarrierSyncer(name string, startedWG *sync.WaitGroup, releaseCh <-chan struct{}) FuncSyncer {
	return FuncSyncer{name: name, fn: func(ctx context.Context) error {
		if startedWG != nil {
			startedWG.Done()
		}
		select {
		case <-releaseCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

// NewErrorSyncer returns a syncer that signals startedWG.Done() when Sync begins,
// then blocks until trigger is closed (returns errToReturn) or ctx is canceled (returns ctx.Err).
func NewErrorSyncer(name string, startedWG *sync.WaitGroup, trigger <-chan struct{}, errToReturn error) FuncSyncer {
	return FuncSyncer{name: name, fn: func(ctx context.Context) error {
		if startedWG != nil {
			startedWG.Done()
		}
		select {
		case <-trigger:
			return errToReturn
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
}

// NewCancelAwareSyncer returns a syncer that signals startedWG.Done() when Sync begins,
// then blocks until ctx is canceled (returns ctx.Err) or timeout elapses (returns timeout error).
func NewCancelAwareSyncer(name string, startedWG *sync.WaitGroup, timeout time.Duration) FuncSyncer {
	return FuncSyncer{name: name, fn: func(ctx context.Context) error {
		if startedWG != nil {
			startedWG.Done()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(timeout):
			return errors.New("syncer timed out waiting for cancellation")
		}
	}}
}
