// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"cmp"
	"context"
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
	_ ValidatorSet    = (*Validators)(nil)
	_ ValidatorSubset = (*Validators)(nil)
	_ NodeSampler     = (*Validators)(nil)
)

type ValidatorSet interface {
	Has(ctx context.Context, nodeID ids.NodeID) bool // TODO return error
}

type ValidatorSubset interface {
	Top(ctx context.Context, percentage float64) []ids.NodeID // TODO return error
}

func NewValidators(
	peers *Peers,
	log logging.Logger,
	subnetID ids.ID,
	validators validators.State,
	maxValidatorSetStaleness time.Duration,
) *Validators {
	return &Validators{
		peers:                    peers,
		log:                      log,
		subnetID:                 subnetID,
		validators:               validators,
		maxValidatorSetStaleness: maxValidatorSetStaleness,
	}
}

// Validators contains a set of nodes that are staking.
type Validators struct {
	peers                    *Peers
	log                      logging.Logger
	subnetID                 ids.ID
	validators               validators.State
	maxValidatorSetStaleness time.Duration

	lock          sync.Mutex
	validatorList []validator
	validatorSet  set.Set[ids.NodeID]
	totalWeight   uint64
	lastUpdated   time.Time
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

func (v *Validators) refresh(ctx context.Context) {
	if time.Since(v.lastUpdated) < v.maxValidatorSetStaleness {
		return
	}

	// Even though validatorList may be nil, truncating will not panic.
	v.validatorList = v.validatorList[:0]
	v.validatorSet.Clear()
	v.totalWeight = 0

	height, err := v.validators.GetCurrentHeight(ctx)
	if err != nil {
		v.log.Warn("failed to get current height", zap.Error(err))
		return
	}
	validatorSet, err := v.validators.GetValidatorSet(ctx, height, v.subnetID)
	if err != nil {
		v.log.Warn("failed to get validator set", zap.Error(err))
		return
	}

	delete(validatorSet, ids.EmptyNodeID) // Ignore inactive ACP-77 validators.

	for nodeID, vdr := range validatorSet {
		v.validatorList = append(v.validatorList, validator{
			nodeID: nodeID,
			weight: vdr.Weight,
		})
		v.validatorSet.Add(nodeID)
		v.totalWeight += vdr.Weight
	}
	utils.Sort(v.validatorList)

	v.lastUpdated = time.Now()
}

// Sample returns a random sample of connected validators
func (v *Validators) Sample(ctx context.Context, limit int) []ids.NodeID {
	v.lock.Lock()
	defer v.lock.Unlock()

	v.refresh(ctx)

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
		if !v.peers.has(nodeID) {
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

	v.lock.Lock()
	defer v.lock.Unlock()

	v.refresh(ctx)

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
	v.lock.Lock()
	defer v.lock.Unlock()

	v.refresh(ctx)

	return v.peers.has(nodeID) && v.validatorSet.Contains(nodeID)
}
