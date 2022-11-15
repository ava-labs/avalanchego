// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

const (
	// If, when the validator set is reset, cap(set)/len(set) > MaxExcessCapacityFactor,
	// the underlying arrays' capacities will be reduced by a factor of capacityReductionFactor.
	// Higher value for maxExcessCapacityFactor --> less aggressive array downsizing --> less memory allocations
	// but more unnecessary data in the underlying array that can't be garbage collected.
	// Higher value for capacityReductionFactor --> more aggressive array downsizing --> more memory allocations
	// but less unnecessary data in the underlying array that can't be garbage collected.
	maxExcessCapacityFactor = 4
	capacityReductionFactor = 2
)

var _ Set = (*set)(nil)

// Set of validators that can be sampled
type Set interface {
	formatting.PrefixedStringer

	// Set removes all the current validators and adds all the provided
	// validators to the set.
	Set([]Validator) error

	// AddWeight to a staker.
	AddWeight(ids.NodeID, uint64) error

	// GetWeight retrieves the validator weight from the set.
	GetWeight(ids.NodeID) (uint64, bool)

	// SubsetWeight returns the sum of the weights of the validators.
	SubsetWeight(ids.NodeIDSet) (uint64, error)

	// RemoveWeight from a staker.
	RemoveWeight(ids.NodeID, uint64) error

	// Contains returns true if there is a validator with the specified ID
	// currently in the set.
	Contains(ids.NodeID) bool

	// Len returns the number of validators currently in the set.
	Len() int

	// List all the validators in this group
	List() []Validator

	// Weight returns the cumulative weight of all validators in the set.
	Weight() uint64

	// Sample returns a collection of validators, potentially with duplicates.
	// If sampling the requested size isn't possible, an error will be returned.
	Sample(size int) ([]Validator, error)

	// When a validator's weight changes, or a validator is added/removed,
	// this listener is called.
	RegisterCallbackListener(SetCallbackListener)
}

type SetCallbackListener interface {
	OnValidatorAdded(validatorID ids.NodeID, weight uint64)
	OnValidatorRemoved(validatorID ids.NodeID, weight uint64)
	OnValidatorWeightChanged(validatorID ids.NodeID, oldWeight, newWeight uint64)
}

// NewSet returns a new, empty set of validators.
func NewSet() Set {
	return &set{
		vdrs:    make(map[ids.NodeID]*validator),
		sampler: sampler.NewWeightedWithoutReplacement(),
	}
}

// NewBestSet returns a new, empty set of validators.
func NewBestSet(expectedSampleSize int) Set {
	return &set{
		vdrs:    make(map[ids.NodeID]*validator),
		sampler: sampler.NewBestWeightedWithoutReplacement(expectedSampleSize),
	}
}

// set of validators. Validator function results are cached. Therefore, to
// update a validators weight, one should ensure to call add with the updated
// validator.
type set struct {
	lock        sync.RWMutex
	vdrs        map[ids.NodeID]*validator
	vdrSlice    []*validator
	weights     []uint64
	totalWeight uint64

	samplerInitialized bool
	sampler            sampler.WeightedWithoutReplacement

	callbackListeners []SetCallbackListener
}

func (s *set) Set(vdrs []Validator) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.set(vdrs)
}

func (s *set) set(vdrs []Validator) error {
	// find all the nodes that are going to be added or have their weight changed
	nodesInResultSet := ids.NewNodeIDSet(len(vdrs))
	for _, vdr := range vdrs {
		vdrID := vdr.ID()
		if nodesInResultSet.Contains(vdrID) {
			continue
		}
		nodesInResultSet.Add(vdrID)

		newWeight := vdr.Weight()
		vdr, contains := s.vdrs[vdrID]
		if !contains {
			s.callValidatorAddedCallbacks(vdrID, newWeight)
			continue
		}

		existingWeight := vdr.weight
		if existingWeight != newWeight {
			s.callWeightChangeCallbacks(vdrID, existingWeight, newWeight)
		}
	}

	// find all nodes that are going to be removed
	for _, vdr := range s.vdrSlice {
		if !nodesInResultSet.Contains(vdr.nodeID) {
			s.callValidatorRemovedCallbacks(vdr.nodeID, vdr.weight)
		}
	}

	lenVdrs := len(vdrs)
	// If the underlying arrays are much larger than necessary, resize them to
	// allow garbage collection of unused memory
	if cap(s.vdrSlice) > len(s.vdrSlice)*maxExcessCapacityFactor {
		newCap := cap(s.vdrSlice) / capacityReductionFactor
		if newCap < lenVdrs {
			newCap = lenVdrs
		}
		s.vdrSlice = make([]*validator, 0, newCap)
		s.weights = make([]uint64, 0, newCap)
	} else {
		s.vdrSlice = s.vdrSlice[:0]
		s.weights = s.weights[:0]
	}

	s.vdrs = make(map[ids.NodeID]*validator, lenVdrs)
	s.totalWeight = 0
	s.samplerInitialized = false

	for _, vdr := range vdrs {
		vdrID := vdr.ID()
		if s.contains(vdrID) {
			continue
		}
		w := vdr.Weight()
		if w == 0 {
			continue // This validator would never be sampled anyway
		}

		newVdr := &validator{
			nodeID: vdrID,
			weight: w,
			index:  len(s.vdrSlice),
		}
		s.vdrs[vdrID] = newVdr
		s.vdrSlice = append(s.vdrSlice, newVdr)
		s.weights = append(s.weights, w)

		newTotalWeight, err := math.Add64(s.totalWeight, w)
		if err != nil {
			return err
		}
		s.totalWeight = newTotalWeight
	}
	return nil
}

func (s *set) AddWeight(vdrID ids.NodeID, weight uint64) error {
	if weight == 0 {
		return nil // This validator would never be sampled anyway
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	return s.addWeight(vdrID, weight)
}

func (s *set) addWeight(vdrID ids.NodeID, weight uint64) error {
	vdr, nodeExists := s.vdrs[vdrID]
	if !nodeExists {
		vdr = &validator{
			nodeID: vdrID,
			index:  len(s.vdrSlice),
		}
		s.vdrs[vdrID] = vdr
		s.vdrSlice = append(s.vdrSlice, vdr)
		s.weights = append(s.weights, 0)

		s.callValidatorAddedCallbacks(vdrID, weight)
	}

	oldWeight := vdr.weight
	s.weights[vdr.index] += weight
	vdr.addWeight(weight)

	if nodeExists {
		s.callWeightChangeCallbacks(vdrID, oldWeight, vdr.weight)
	}

	newTotalWeight, err := math.Add64(s.totalWeight, weight)
	if err != nil {
		return nil
	}
	s.totalWeight = newTotalWeight
	s.samplerInitialized = false
	return nil
}

func (s *set) GetWeight(vdrID ids.NodeID) (uint64, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.getWeight(vdrID)
}

func (s *set) getWeight(vdrID ids.NodeID) (uint64, bool) {
	if vdr, ok := s.vdrs[vdrID]; ok {
		return vdr.weight, true
	}
	return 0, false
}

func (s *set) SubsetWeight(subset ids.NodeIDSet) (uint64, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	totalWeight := uint64(0)
	for vdrID := range subset {
		weight, ok := s.getWeight(vdrID)
		if !ok {
			continue
		}
		newWeight, err := math.Add64(totalWeight, weight)
		if err != nil {
			return 0, err
		}
		totalWeight = newWeight
	}
	return totalWeight, nil
}

func (s *set) RemoveWeight(vdrID ids.NodeID, weight uint64) error {
	if weight == 0 {
		return nil
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	return s.removeWeight(vdrID, weight)
}

func (s *set) removeWeight(vdrID ids.NodeID, weight uint64) error {
	vdr, ok := s.vdrs[vdrID]
	if !ok {
		return nil
	}

	// Validator exists

	oldWeight := vdr.weight
	weight = math.Min(oldWeight, weight)
	s.weights[vdr.index] -= weight
	vdr.removeWeight(weight)
	s.totalWeight -= weight

	if vdr.Weight() == 0 {
		s.callValidatorRemovedCallbacks(vdrID, oldWeight)
		if err := s.remove(vdrID); err != nil {
			return err
		}
	} else {
		s.callWeightChangeCallbacks(vdrID, oldWeight, vdr.weight)
	}
	s.samplerInitialized = false
	return nil
}

func (s *set) Get(vdrID ids.NodeID) (Validator, bool) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.get(vdrID)
}

func (s *set) get(vdrID ids.NodeID) (Validator, bool) {
	vdr, ok := s.vdrs[vdrID]
	return vdr, ok
}

func (s *set) remove(vdrID ids.NodeID) error {
	// Get the element to remove
	vdrToRemove, contains := s.vdrs[vdrID]
	if !contains {
		return nil
	}

	// Get the last element
	lastIndex := len(s.vdrSlice) - 1
	vdrToSwap := s.vdrSlice[lastIndex]

	// Move element at last index --> index of removed validator
	vdrToSwap.index = vdrToRemove.index
	s.vdrSlice[vdrToRemove.index] = vdrToSwap
	s.weights[vdrToRemove.index] = vdrToSwap.weight

	// Remove validator
	delete(s.vdrs, vdrID)
	s.vdrSlice[lastIndex] = nil
	s.vdrSlice = s.vdrSlice[:lastIndex]
	s.weights = s.weights[:lastIndex]

	newTotalWeight, err := math.Sub(s.totalWeight, vdrToRemove.weight)
	if err != nil {
		return err
	}
	s.totalWeight = newTotalWeight
	s.samplerInitialized = false
	return nil
}

func (s *set) Contains(vdrID ids.NodeID) bool {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.contains(vdrID)
}

func (s *set) contains(vdrID ids.NodeID) bool {
	_, contains := s.vdrs[vdrID]
	return contains
}

func (s *set) Len() int {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.len()
}

func (s *set) len() int {
	return len(s.vdrSlice)
}

func (s *set) List() []Validator {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.list()
}

func (s *set) list() []Validator {
	list := make([]Validator, len(s.vdrSlice))
	for i, vdr := range s.vdrSlice {
		list[i] = vdr
	}
	return list
}

func (s *set) Sample(size int) ([]Validator, error) {
	if size == 0 {
		return nil, nil
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	return s.sample(size)
}

func (s *set) sample(size int) ([]Validator, error) {
	if !s.samplerInitialized {
		if err := s.sampler.Initialize(s.weights); err != nil {
			return nil, err
		}
		s.samplerInitialized = true
	}

	indices, err := s.sampler.Sample(size)
	if err != nil {
		return nil, err
	}

	list := make([]Validator, size)
	for i, index := range indices {
		list[i] = s.vdrSlice[index]
	}
	return list, nil
}

func (s *set) Weight() uint64 {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.totalWeight
}

func (s *set) String() string {
	return s.PrefixedString("")
}

func (s *set) PrefixedString(prefix string) string {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.prefixedString(prefix)
}

func (s *set) prefixedString(prefix string) string {
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
			vdr.ID(),
			vdr.Weight(),
		))
	}

	return sb.String()
}

func (s *set) RegisterCallbackListener(callbackListener SetCallbackListener) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.callbackListeners = append(s.callbackListeners, callbackListener)
	for _, vdr := range s.vdrSlice {
		callbackListener.OnValidatorAdded(vdr.nodeID, vdr.weight)
	}
}

// Assumes [s.lock] is held
func (s *set) callWeightChangeCallbacks(node ids.NodeID, oldWeight, newWeight uint64) {
	for _, callbackListener := range s.callbackListeners {
		callbackListener.OnValidatorWeightChanged(node, oldWeight, newWeight)
	}
}

// Assumes [s.lock] is held
func (s *set) callValidatorAddedCallbacks(node ids.NodeID, weight uint64) {
	for _, callbackListener := range s.callbackListeners {
		callbackListener.OnValidatorAdded(node, weight)
	}
}

// Assumes [s.lock] is held
func (s *set) callValidatorRemovedCallbacks(node ids.NodeID, weight uint64) {
	for _, callbackListener := range s.callbackListeners {
		callbackListener.OnValidatorRemoved(node, weight)
	}
}
