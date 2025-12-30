// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ ValidatorSet      = (*Validators)(nil)
	_ ValidatorSubset   = (*Validators)(nil)
	_ NodeSampler       = (*Validators)(nil)
	_ ConnectionHandler = (*Validators)(nil)
)

type ValidatorSet interface {
	Len(ctx context.Context) int
	Has(ctx context.Context, nodeID ids.NodeID) bool // TODO return error
}

type ValidatorSubset interface {
	Top(ctx context.Context, percentage float64) []ids.NodeID // TODO return error
}

func NewValidators(
	log logging.Logger,
	subnetID ids.ID,
	validators validators.State,
	maxValidatorSetStaleness time.Duration,
) *Validators {
	return &Validators{
		log:                      log,
		subnetID:                 subnetID,
		validators:               validators,
		maxValidatorSetStaleness: maxValidatorSetStaleness,
	}
}

// Validators contains a set of nodes that are staking.
type Validators struct {
	log                      logging.Logger
	subnetID                 ids.ID
	validators               validators.State
	maxValidatorSetStaleness time.Duration

	lock                sync.RWMutex
	peers               set.Set[ids.NodeID]
	connectedValidators set.Set[ids.NodeID]
	validatorList       []validator
	validatorSet        set.Set[ids.NodeID]
	totalWeight         uint64
	lastUpdated         time.Time
}

type validator struct {
	nodeID ids.NodeID
	weight uint64
}

func (v validator) Compare(other validator) int {
	if weightCmp := cmp.Compare(v.weight, other.weight); weightCmp != 0 {
		return -weightCmp // Sort in decreasing order of stake
	}
	return v.nodeID.Compare(other.nodeID)
}

// getCurrentValidators must not be called with Validators.lock held to avoid a
// potential deadlock.
//
// getCurrentValidators calls [validators.State] which grabs the context lock.
// [Validators.Connected] and [Validators.Disconnected] are called with the
// context lock.
func (v *Validators) getCurrentValidators(ctx context.Context) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	height, err := v.validators.GetCurrentHeight(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting current height: %w", err)
	}
	validatorSet, err := v.validators.GetValidatorSet(ctx, height, v.subnetID)
	if err != nil {
		return nil, fmt.Errorf("getting validator set: %w", err)
	}
	delete(validatorSet, ids.EmptyNodeID) // Ignore inactive ACP-77 validators.
	return validatorSet, nil
}

// refresh must not be called with Validators.lock held.
func (v *Validators) refresh(ctx context.Context) {
	v.lock.RLock()
	lastUpdated := v.lastUpdated
	v.lock.RUnlock()

	if time.Since(lastUpdated) < v.maxValidatorSetStaleness {
		return
	}

	validatorSet, err := v.getCurrentValidators(ctx)
	if err != nil {
		v.log.Warn("failed to get current validator set", zap.Error(err))
		return
	}

	v.lock.Lock()
	defer v.lock.Unlock()

	// Even though validatorList may be nil, truncating will not panic.
	v.validatorList = v.validatorList[:0]
	v.validatorSet.Clear()
	v.totalWeight = 0
	for nodeID, vdr := range validatorSet {
		v.validatorList = append(v.validatorList, validator{
			nodeID: nodeID,
			weight: vdr.Weight,
		})
		v.validatorSet.Add(nodeID)
		v.totalWeight += vdr.Weight
	}
	utils.Sort(v.validatorList)

	v.connectedValidators = set.Intersect(v.validatorSet, v.peers)

	v.lastUpdated = time.Now()
}

// Sample returns a random sample of connected validators
func (v *Validators) Sample(ctx context.Context, limit int) []ids.NodeID {
	v.refresh(ctx)

	v.lock.RLock()
	defer v.lock.RUnlock()

	var (
		uniform = sampler.NewUniform()
		sampled = make([]ids.NodeID, 0, limit)
	)

	uniform.Initialize(uint64(len(v.validatorList)))
	for len(sampled) < limit {
		i, hasNext := uniform.Next()
		if !hasNext {
			break
		}

		nodeID := v.validatorList[i].nodeID
		if !v.peers.Contains(nodeID) {
			continue
		}

		sampled = append(sampled, nodeID)
	}

	return sampled
}

// Top returns the top [percentage] of validators, regardless of if they are
// connected or not.
func (v *Validators) Top(ctx context.Context, percentage float64) []ids.NodeID {
	percentage = max(0, min(1, percentage)) // bound percentage inside [0, 1]

	v.refresh(ctx)

	v.lock.RLock()
	defer v.lock.RUnlock()

	var (
		maxSize      = int(math.Ceil(percentage * float64(len(v.validatorList))))
		top          = make([]ids.NodeID, 0, maxSize)
		currentStake uint64
		targetStake  = uint64(math.Ceil(percentage * float64(v.totalWeight)))
	)

	for _, vdr := range v.validatorList {
		if currentStake >= targetStake {
			break
		}
		top = append(top, vdr.nodeID)
		currentStake += vdr.weight
	}

	return top
}

// Has returns if nodeID is a connected validator
func (v *Validators) Has(ctx context.Context, nodeID ids.NodeID) bool {
	v.refresh(ctx)

	v.lock.RLock()
	defer v.lock.RUnlock()

	return v.connectedValidators.Contains(nodeID)
}

// Len returns the number of connected validators.
func (v *Validators) Len(ctx context.Context) int {
	v.refresh(ctx)

	v.lock.RLock()
	defer v.lock.RUnlock()

	return v.connectedValidators.Len()
}

func (v *Validators) Connected(nodeID ids.NodeID) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.peers.Add(nodeID)

	if !v.validatorSet.Contains(nodeID) {
		return
	}

	v.connectedValidators.Add(nodeID)
}

func (v *Validators) Disconnected(nodeID ids.NodeID) {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.peers.Remove(nodeID)
	v.connectedValidators.Remove(nodeID)
}
