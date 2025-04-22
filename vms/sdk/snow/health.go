// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	vmReadinessHealthChecker      = "snowVMReady"
	unresolvedBlocksHealthChecker = "snowUnresolvedBlocks"
)

var (
	errUnresolvedBlocks = errors.New("unresolved invalid blocks in processing")
	errVMNotReady       = errors.New("vm not ready")
)

func (v *VM[I, O, A]) HealthCheck(ctx context.Context) (any, error) {
	var (
		details = make(map[string]any)
		errs    []error
	)

	v.healthCheckers.Range(func(k, v any) bool {
		name := k.(string)
		checker := v.(health.Checker)
		checkerDetails, err := checker.HealthCheck(ctx)

		details[name] = checkerDetails
		errs = append(errs, err)
		return true
	})

	return details, errors.Join(errs...)
}

func (v *VM[I, O, A]) RegisterHealthChecker(name string, healthChecker health.Checker) error {
	if _, ok := v.healthCheckers.LoadOrStore(name, healthChecker); ok {
		return fmt.Errorf("duplicate health checker for %s", name)
	}

	return nil
}

func (v *VM[I, O, A]) initHealthCheckers() error {
	vmReadiness := newVMReadinessHealthCheck(func() bool {
		return v.ready
	})
	return v.RegisterHealthChecker(vmReadinessHealthChecker, vmReadiness)
}

// vmReadinessHealthCheck marks itself as ready iff the VM is in normal operation.
// ie. has the full state required to process new blocks from tip.
type vmReadinessHealthCheck struct {
	isReady func() bool
}

func newVMReadinessHealthCheck(isReady func() bool) *vmReadinessHealthCheck {
	return &vmReadinessHealthCheck{isReady: isReady}
}

func (v *vmReadinessHealthCheck) HealthCheck(_ context.Context) (any, error) {
	ready := v.isReady()
	if !ready {
		return ready, errVMNotReady
	}
	return ready, nil
}

// unresolvedBlockHealthCheck
// During state sync, blocks are vacuously marked as verified because the VM lacks the state required
// to properly verify them.
// Assuming a correct validator set and consensus, any invalid blocks will eventually be rejected by
// the network and this node.
// This check reports unhealthy until any such blocks have been cleared from the processing set.
type unresolvedBlockHealthCheck[I Block] struct {
	lock             sync.RWMutex
	unresolvedBlocks set.Set[ids.ID]
}

func newUnresolvedBlocksHealthCheck[I Block](unresolvedBlkIDs set.Set[ids.ID]) *unresolvedBlockHealthCheck[I] {
	return &unresolvedBlockHealthCheck[I]{
		unresolvedBlocks: unresolvedBlkIDs,
	}
}

func (u *unresolvedBlockHealthCheck[I]) Resolve(blkID ids.ID) {
	u.lock.Lock()
	defer u.lock.Unlock()

	u.unresolvedBlocks.Remove(blkID)
}

func (u *unresolvedBlockHealthCheck[I]) HealthCheck(_ context.Context) (any, error) {
	u.lock.RLock()
	unresolvedBlocks := u.unresolvedBlocks.Len()
	u.lock.RUnlock()

	if unresolvedBlocks > 0 {
		return unresolvedBlocks, fmt.Errorf("%w: %d", errUnresolvedBlocks, unresolvedBlocks)
	}
	return unresolvedBlocks, nil
}
