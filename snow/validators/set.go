// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/sampler"

	safemath "github.com/ava-labs/gecko/utils/math"
)

const (
	// maxExcessCapacityFactor ...
	// If, when the validator set is reset, cap(set)/len(set) > MaxExcessCapacityFactor,
	// the underlying arrays' capacities will be reduced by a factor of capacityReductionFactor.
	// Higher value for maxExcessCapacityFactor --> less aggressive array downsizing --> less memory allocations
	// but more unnecessary data in the underlying array that can't be garbage collected.
	// Higher value for capacityReductionFactor --> more aggressive array downsizing --> more memory allocations
	// but less unnecessary data in the underlying array that can't be garbage collected.
	maxExcessCapacityFactor = 4
	// CapacityReductionFactor ...
	capacityReductionFactor = 2
)

// Set of validators that can be sampled
type Set interface {
	fmt.Stringer

	// Set removes all the current validators and adds all the provided
	// validators to the set.
	Set([]Validator) error

	// Add the provided validator to the set.
	Add(Validator) error

	// Get the validator from the set.
	Get(ids.ShortID) (Validator, bool)

	// Remove the validator with the specified ID.
	Remove(ids.ShortID) error

	// Contains returns true if there is a validator with the specified ID
	// currently in the set.
	Contains(ids.ShortID) bool

	// Len returns the number of validators currently in the set.
	Len() int

	// List all the validators in this group
	List() []Validator

	// Weight returns the cumulative weight of all validators in the set.
	Weight() uint64

	// Sample returns a collection of validators, potentially with duplicates.
	// If sampling the requested size isn't possible, an error will be returned.
	Sample(size int) ([]Validator, error)
}

// NewSet returns a new, empty set of validators.
func NewSet() Set {
	return &set{
		vdrMap:  make(map[[20]byte]int),
		sampler: sampler.NewWeightedWithoutReplacement(),
	}
}

// set of validators. Validator function results are cached. Therefore, to
// update a validators weight, one should ensure to call add with the updated
// validator.
type set struct {
	lock        sync.Mutex
	vdrMap      map[[20]byte]int
	vdrSlice    []Validator
	vdrWeights  []uint64
	sampler     sampler.WeightedWithoutReplacement
	totalWeight uint64
}

// Set implements the Set interface.
func (s *set) Set(vdrs []Validator) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.set(vdrs)
}

func (s *set) set(vdrs []Validator) error {
	lenVdrs := len(vdrs)
	// If the underlying arrays are much larger than necessary, resize them to
	// allow garbage collection of unused memory
	if cap(s.vdrSlice) > len(s.vdrSlice)*maxExcessCapacityFactor {
		newCap := cap(s.vdrSlice) / capacityReductionFactor
		if newCap < lenVdrs {
			newCap = lenVdrs
		}
		s.vdrSlice = make([]Validator, 0, newCap)
		s.vdrWeights = make([]uint64, 0, newCap)
	} else {
		s.vdrSlice = s.vdrSlice[:0]
		s.vdrWeights = s.vdrWeights[:0]
	}
	s.vdrMap = make(map[[20]byte]int, lenVdrs)
	s.totalWeight = 0

	for _, vdr := range vdrs {
		vdrID := vdr.ID()
		if s.contains(vdrID) {
			continue
		}

		w := vdr.Weight()
		if w == 0 {
			continue // This validator would never be sampled anyway
		}

		i := len(s.vdrSlice)
		s.vdrMap[vdrID.Key()] = i
		s.vdrSlice = append(s.vdrSlice, vdr)
		s.vdrWeights = append(s.vdrWeights, w)
		newTotalWeight, err := safemath.Add64(s.totalWeight, w)
		if err != nil {
			return err
		}
		s.totalWeight = newTotalWeight
	}
	return s.sampler.Initialize(s.vdrWeights)
}

// Add implements the Set interface.
func (s *set) Add(vdr Validator) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.add(vdr)
}

func (s *set) add(vdr Validator) error {
	vdrID := vdr.ID()
	if s.contains(vdrID) {
		if err := s.remove(vdrID); err != nil {
			return err
		}
	}

	w := vdr.Weight()
	if w == 0 {
		return nil // This validator would never be sampled anyway
	}

	i := len(s.vdrSlice)
	s.vdrMap[vdrID.Key()] = i
	s.vdrSlice = append(s.vdrSlice, vdr)
	s.vdrWeights = append(s.vdrWeights, w)
	newTotalWeight, err := safemath.Add64(s.totalWeight, w)
	if err != nil {
		return err
	}
	s.totalWeight = newTotalWeight
	return s.sampler.Initialize(s.vdrWeights)
}

// Get implements the Set interface.
func (s *set) Get(vdrID ids.ShortID) (Validator, bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.get(vdrID)
}

func (s *set) get(vdrID ids.ShortID) (Validator, bool) {
	index, ok := s.vdrMap[vdrID.Key()]
	if !ok {
		return nil, false
	}
	return s.vdrSlice[index], true
}

// Remove implements the Set interface.
func (s *set) Remove(vdrID ids.ShortID) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.remove(vdrID)
}

func (s *set) remove(vdrID ids.ShortID) error {
	// Get the element to remove
	iKey := vdrID.Key()
	i, contains := s.vdrMap[iKey]
	if !contains {
		return nil
	}

	// Get the last element
	e := len(s.vdrSlice) - 1
	eVdr := s.vdrSlice[e]
	eKey := eVdr.ID().Key()

	// Move e -> i
	iElem := s.vdrSlice[i]
	s.vdrMap[eKey] = i
	s.vdrSlice[i] = eVdr
	s.vdrWeights[i] = eVdr.Weight()

	// Remove i
	delete(s.vdrMap, iKey)
	s.vdrSlice = s.vdrSlice[:e]
	s.vdrWeights = s.vdrWeights[:e]

	newTotalWeight, err := safemath.Sub64(s.totalWeight, iElem.Weight())
	if err != nil {
		return err
	}
	s.totalWeight = newTotalWeight
	return s.sampler.Initialize(s.vdrWeights)
}

// Contains implements the Set interface.
func (s *set) Contains(vdrID ids.ShortID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.contains(vdrID)
}

func (s *set) contains(vdrID ids.ShortID) bool {
	_, contains := s.vdrMap[vdrID.Key()]
	return contains
}

// Len implements the Set interface.
func (s *set) Len() int {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.len()
}

func (s *set) len() int { return len(s.vdrSlice) }

// List implements the Group interface.
func (s *set) List() []Validator {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.list()
}

func (s *set) list() []Validator {
	list := make([]Validator, len(s.vdrSlice))
	copy(list, s.vdrSlice)
	return list
}

// Sample implements the Group interface.
func (s *set) Sample(size int) ([]Validator, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.sample(size)
}

func (s *set) sample(size int) ([]Validator, error) {
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
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.totalWeight
}

func (s *set) calculateWeight() (uint64, error) {
	weight := uint64(0)
	for _, vdr := range s.vdrSlice {
		weight, err := safemath.Add64(weight, vdr.Weight())
		if err != nil {
			return weight, err
		}
	}
	return weight, nil
}

func (s *set) String() string {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.string()
}

func (s *set) string() string {
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("Validator Set: (Size = %d)", len(s.vdrSlice)))
	format := fmt.Sprintf("\n    Validator[%s]: %%33s, %%d", formatting.IntFormat(len(s.vdrSlice)-1))
	for i, vdr := range s.vdrSlice {
		sb.WriteString(fmt.Sprintf(format, i, vdr.ID(), vdr.Weight()))
	}

	return sb.String()
}
