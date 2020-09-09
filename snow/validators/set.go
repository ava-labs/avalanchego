// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/formatting"
	safemath "github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/utils/sampler"
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

	// AddWeight to a staker.
	AddWeight(ids.ShortID, uint64) error

	// Get the validator from the set.
	GetWeight(ids.ShortID) (uint64, bool)

	// RemoveWeight from a staker.
	RemoveWeight(ids.ShortID, uint64) error

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

// NewBestSet returns a new, empty set of validators.
func NewBestSet(expectedSampleSize int) Set {
	return &set{
		vdrMap:  make(map[[20]byte]int),
		sampler: sampler.NewBestWeightedWithoutReplacement(expectedSampleSize),
	}
}

// set of validators. Validator function results are cached. Therefore, to
// update a validators weight, one should ensure to call add with the updated
// validator.
type set struct {
	lock        sync.Mutex
	vdrMap      map[[20]byte]int
	vdrSlice    []*validator
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
		s.vdrSlice = make([]*validator, 0, newCap)
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
		s.vdrSlice = append(s.vdrSlice, &validator{
			nodeID: vdr.ID(),
			weight: vdr.Weight(),
		})
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
func (s *set) AddWeight(vdrID ids.ShortID, weight uint64) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.addWeight(vdrID, weight)
}

func (s *set) addWeight(vdrID ids.ShortID, weight uint64) error {
	if weight == 0 {
		return nil // This validator would never be sampled anyway
	}

	newTotalWeight, err := safemath.Add64(s.totalWeight, weight)
	if err != nil {
		return nil
	}
	s.totalWeight = newTotalWeight

	vdrIDKey := vdrID.Key()

	var vdr *validator
	i, ok := s.vdrMap[vdrIDKey]
	if !ok {
		vdr = &validator{
			nodeID: vdrID,
		}
		i = len(s.vdrSlice)
		s.vdrSlice = append(s.vdrSlice, vdr)
		s.vdrWeights = append(s.vdrWeights, 0)
		s.vdrMap[vdrIDKey] = i
	} else {
		vdr = s.vdrSlice[i]
	}

	s.vdrWeights[i] += weight
	vdr.addWeight(weight)
	return s.sampler.Initialize(s.vdrWeights)
}

// GetWeight implements the Set interface.
func (s *set) GetWeight(vdrID ids.ShortID) (uint64, bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.getWeight(vdrID)
}

func (s *set) getWeight(vdrID ids.ShortID) (uint64, bool) {
	if index, ok := s.vdrMap[vdrID.Key()]; ok {
		return s.vdrWeights[index], true
	}
	return 0, false
}

// RemoveWeight implements the Set interface.
func (s *set) RemoveWeight(vdrID ids.ShortID, weight uint64) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.removeWeight(vdrID, weight)
}

func (s *set) removeWeight(vdrID ids.ShortID, weight uint64) error {
	if weight == 0 {
		return nil
	}

	i, ok := s.vdrMap[vdrID.Key()]
	if !ok {
		return nil
	}

	// Validator exists
	vdr := s.vdrSlice[i]

	weight = safemath.Min64(s.vdrWeights[i], weight)
	s.vdrWeights[i] -= weight
	s.totalWeight -= weight
	vdr.removeWeight(weight)

	if vdr.Weight() == 0 {
		if err := s.remove(vdrID); err != nil {
			return err
		}
	}
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
	for i, vdr := range s.vdrSlice {
		list[i] = vdr
	}
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
