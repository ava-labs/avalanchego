// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	errDuplicateValidator   = errors.New("duplicate validator")
	errMissingValidator     = errors.New("missing validator")
	errTotalWeightNotUint64 = errors.New("total weight is not a uint64")
	errInsufficientWeight   = errors.New("insufficient weight")
)

// newSet returns a new, empty set of validators.
func newSet(subnetID ids.ID, callbackListeners []ManagerCallbackListener) *vdrSet {
	return &vdrSet{
		subnetID:                 subnetID,
		vdrs:                     make(map[ids.NodeID]*Validator),
		totalWeight:              new(big.Int),
		sampler:                  sampler.NewWeightedWithoutReplacement(),
		managerCallbackListeners: slices.Clone(callbackListeners),
	}
}

type vdrSet struct {
	subnetID ids.ID

	lock        sync.RWMutex
	vdrs        map[ids.NodeID]*Validator
	vdrSlice    []*Validator
	weights     []uint64
	totalWeight *big.Int

	samplerInitialized bool
	sampler            sampler.WeightedWithoutReplacement

	managerCallbackListeners []ManagerCallbackListener
	setCallbackListeners     []SetCallbackListener
}

func (s *vdrSet) Add(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.add(nodeID, pk, txID, weight)
}

func (s *vdrSet) add(nodeID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) error {
	_, nodeExists := s.vdrs[nodeID]
	if nodeExists {
		return errDuplicateValidator
	}

	vdr := &Validator{
		NodeID:    nodeID,
		PublicKey: pk,
		TxID:      txID,
		Weight:    weight,
		index:     len(s.vdrSlice),
	}
	s.vdrs[nodeID] = vdr
	s.vdrSlice = append(s.vdrSlice, vdr)
	s.weights = append(s.weights, weight)
	s.totalWeight.Add(s.totalWeight, new(big.Int).SetUint64(weight))
	s.samplerInitialized = false

	s.callValidatorAddedCallbacks(nodeID, pk, txID, weight)
	return nil
}

func (s *vdrSet) AddWeight(nodeID ids.NodeID, weight uint64) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.addWeight(nodeID, weight)
}

func (s *vdrSet) addWeight(nodeID ids.NodeID, weight uint64) error {
	vdr, nodeExists := s.vdrs[nodeID]
	if !nodeExists {
		return errMissingValidator
	}

	oldWeight := vdr.Weight
	newWeight, err := math.Add(oldWeight, weight)
	if err != nil {
		return err
	}
	vdr.Weight = newWeight
	s.weights[vdr.index] = newWeight
	s.totalWeight.Add(s.totalWeight, new(big.Int).SetUint64(weight))
	s.samplerInitialized = false

	s.callWeightChangeCallbacks(nodeID, oldWeight, vdr.Weight)
	return nil
}

func (s *vdrSet) GetWeight(nodeID ids.NodeID) uint64 {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getWeight(nodeID)
}

func (s *vdrSet) getWeight(nodeID ids.NodeID) uint64 {
	if vdr, ok := s.vdrs[nodeID]; ok {
		return vdr.Weight
	}
	return 0
}

func (s *vdrSet) SubsetWeight(subset set.Set[ids.NodeID]) (uint64, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.subsetWeight(subset)
}

func (s *vdrSet) subsetWeight(subset set.Set[ids.NodeID]) (uint64, error) {
	var (
		totalWeight uint64
		err         error
	)
	for nodeID := range subset {
		totalWeight, err = math.Add(totalWeight, s.getWeight(nodeID))
		if err != nil {
			return 0, err
		}
	}
	return totalWeight, nil
}

func (s *vdrSet) RemoveWeight(nodeID ids.NodeID, weight uint64) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.removeWeight(nodeID, weight)
}

func (s *vdrSet) removeWeight(nodeID ids.NodeID, weight uint64) error {
	vdr, ok := s.vdrs[nodeID]
	if !ok {
		return errMissingValidator
	}

	oldWeight := vdr.Weight
	// We first calculate the new weight of the validator, as this guarantees
	// that none of the following operations can underflow.
	newWeight, err := math.Sub(oldWeight, weight)
	if err != nil {
		return err
	}

	if newWeight == 0 {
		// Get the last element
		lastIndex := len(s.vdrSlice) - 1
		vdrToSwap := s.vdrSlice[lastIndex]

		// Move element at last index --> index of removed validator
		vdrToSwap.index = vdr.index
		s.vdrSlice[vdr.index] = vdrToSwap
		s.weights[vdr.index] = vdrToSwap.Weight

		// Remove validator
		delete(s.vdrs, nodeID)
		s.vdrSlice[lastIndex] = nil
		s.vdrSlice = s.vdrSlice[:lastIndex]
		s.weights = s.weights[:lastIndex]

		s.callValidatorRemovedCallbacks(nodeID, oldWeight)
	} else {
		vdr.Weight = newWeight
		s.weights[vdr.index] = newWeight

		s.callWeightChangeCallbacks(nodeID, oldWeight, newWeight)
	}
	s.totalWeight.Sub(s.totalWeight, new(big.Int).SetUint64(weight))
	s.samplerInitialized = false
	return nil
}

func (s *vdrSet) Get(nodeID ids.NodeID) (*Validator, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.get(nodeID)
}

func (s *vdrSet) get(nodeID ids.NodeID) (*Validator, bool) {
	vdr, ok := s.vdrs[nodeID]
	if !ok {
		return nil, false
	}
	copiedVdr := *vdr
	return &copiedVdr, true
}

func (s *vdrSet) Len() int {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.len()
}

func (s *vdrSet) len() int {
	return len(s.vdrSlice)
}

func (s *vdrSet) HasCallbackRegistered() bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return len(s.setCallbackListeners) > 0
}

func (s *vdrSet) Map() map[ids.NodeID]*GetValidatorOutput {
	s.lock.RLock()
	defer s.lock.RUnlock()

	set := make(map[ids.NodeID]*GetValidatorOutput, len(s.vdrSlice))
	for _, vdr := range s.vdrSlice {
		set[vdr.NodeID] = &GetValidatorOutput{
			NodeID:    vdr.NodeID,
			PublicKey: vdr.PublicKey,
			Weight:    vdr.Weight,
		}
	}
	return set
}

func (s *vdrSet) Sample(size int) ([]ids.NodeID, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.sample(size)
}

func (s *vdrSet) sample(size int) ([]ids.NodeID, error) {
	if !s.samplerInitialized {
		if err := s.sampler.Initialize(s.weights); err != nil {
			return nil, err
		}
		s.samplerInitialized = true
	}

	indices, ok := s.sampler.Sample(size)
	if !ok {
		return nil, errInsufficientWeight
	}

	list := make([]ids.NodeID, size)
	for i, index := range indices {
		list[i] = s.vdrSlice[index].NodeID
	}
	return list, nil
}

func (s *vdrSet) TotalWeight() (uint64, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if !s.totalWeight.IsUint64() {
		return 0, fmt.Errorf("%w, total weight: %s", errTotalWeightNotUint64, s.totalWeight)
	}

	return s.totalWeight.Uint64(), nil
}

func (s *vdrSet) String() string {
	return s.PrefixedString("")
}

func (s *vdrSet) PrefixedString(prefix string) string {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.prefixedString(prefix)
}

func (s *vdrSet) prefixedString(prefix string) string {
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("Validator Set: (Size = %d, Weight = %d)",
		len(s.vdrSlice),
		s.totalWeight,
	))
	format := fmt.Sprintf("\n%s    Validator[%s]: %%33s, %%d", prefix, formatting.IntFormat(len(s.vdrSlice)-1))
	for i, vdr := range s.vdrSlice {
		sb.WriteString(fmt.Sprintf(
			format,
			i,
			vdr.NodeID,
			vdr.Weight,
		))
	}

	return sb.String()
}

func (s *vdrSet) RegisterManagerCallbackListener(callbackListener ManagerCallbackListener) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.managerCallbackListeners = append(s.managerCallbackListeners, callbackListener)
	for _, vdr := range s.vdrSlice {
		callbackListener.OnValidatorAdded(s.subnetID, vdr.NodeID, vdr.PublicKey, vdr.TxID, vdr.Weight)
	}
}

func (s *vdrSet) RegisterCallbackListener(callbackListener SetCallbackListener) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.setCallbackListeners = append(s.setCallbackListeners, callbackListener)
	for _, vdr := range s.vdrSlice {
		callbackListener.OnValidatorAdded(vdr.NodeID, vdr.PublicKey, vdr.TxID, vdr.Weight)
	}
}

// Assumes [s.lock] is held
func (s *vdrSet) callWeightChangeCallbacks(node ids.NodeID, oldWeight, newWeight uint64) {
	for _, callbackListener := range s.managerCallbackListeners {
		callbackListener.OnValidatorWeightChanged(s.subnetID, node, oldWeight, newWeight)
	}
	for _, callbackListener := range s.setCallbackListeners {
		callbackListener.OnValidatorWeightChanged(node, oldWeight, newWeight)
	}
}

// Assumes [s.lock] is held
func (s *vdrSet) callValidatorAddedCallbacks(node ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	for _, callbackListener := range s.managerCallbackListeners {
		callbackListener.OnValidatorAdded(s.subnetID, node, pk, txID, weight)
	}
	for _, callbackListener := range s.setCallbackListeners {
		callbackListener.OnValidatorAdded(node, pk, txID, weight)
	}
}

// Assumes [s.lock] is held
func (s *vdrSet) callValidatorRemovedCallbacks(node ids.NodeID, weight uint64) {
	for _, callbackListener := range s.managerCallbackListeners {
		callbackListener.OnValidatorRemoved(s.subnetID, node, weight)
	}
	for _, callbackListener := range s.setCallbackListeners {
		callbackListener.OnValidatorRemoved(node, weight)
	}
}

func (s *vdrSet) GetValidatorIDs() []ids.NodeID {
	s.lock.RLock()
	defer s.lock.RUnlock()

	list := make([]ids.NodeID, len(s.vdrSlice))
	for i, vdr := range s.vdrSlice {
		list[i] = vdr.NodeID
	}
	return list
}
