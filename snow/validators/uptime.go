// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/formatting"
	safemath "github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/utils/timer"
)

// Connector represents a handler that is called when a connection is marked as
// connected or disconnected
type Connector interface {
	// returns true if the handler should be removed
	Connected(id ids.ShortID) bool
	Disconnected(id ids.ShortID) bool
}

// DB needs to track:
// {
//     NodeID -> {
//         startTime      time.Time
//         endTime        time.Time
//         lastSaved      time.Time
//         durationOnline time.Duration
//     }
// }

// Uptime ...
type Uptime interface {
	Set
	Connector

	// Uptime returns the percent of time the node thinks the validator has been
	// online.
	Uptime(ids.ShortID) float64

	// Close writes back the uptime metrics.
	Close() error
}

// NewUptime ...
func NewUptime(vdrs Set) (Uptime, error) {
	return &uptime{
		vdrs:       vdrs,
		vdrUptimes: make(map[[20]byte]*validator),
	}, nil
}

type validator struct {
	durationOnline time.Duration
	connected      bool
	timeConnected  time.Time
}

type uptime struct {
	lock sync.Mutex
	timer.Clock
	vdrs       Set
	vdrUptimes map[[20]byte]*validator
	connected  ids.ShortSet
}

// Set implements the Set interface.
func (u *uptime) Set(vdrs []Validator) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.set(vdrs)
}

// Add implements the Set interface.
func (u *uptime) Add(vdr Validator) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.add(vdr)
}

// Get implements the Set interface.
func (u *uptime) Get(vdrID ids.ShortID) (Validator, bool) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.get(vdrID)
}

// Remove implements the Set interface.
func (u *uptime) Remove(vdrID ids.ShortID) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.remove(vdrID)
}

// Contains implements the Set interface.
func (u *uptime) Contains(vdrID ids.ShortID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.contains(vdrID)
}

// Len implements the Set interface.
func (u *uptime) Len() int {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.len()
}

// List implements the Group interface.
func (u *uptime) List() []Validator {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.list()
}

// Sample implements the Group interface.
func (u *uptime) Sample(size int) ([]Validator, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.sample(size)
}

func (u *uptime) Weight() uint64 {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.totalWeight
}

func (u *uptime) String() string {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.string()
}

func (u *uptime) Connected(nodeID ids.ShortID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.string()
}

func (u *uptime) Disconnected(nodeID ids.ShortID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.string()
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

func (s *set) contains(vdrID ids.ShortID) bool {
	_, contains := s.vdrMap[vdrID.Key()]
	return contains
}

func (s *set) len() int { return len(s.vdrSlice) }

func (s *set) list() []Validator {
	list := make([]Validator, len(s.vdrSlice))
	copy(list, s.vdrSlice)
	return list
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

func (s *set) string() string {
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("Validator Set: (Size = %d)", len(s.vdrSlice)))
	format := fmt.Sprintf("\n    Validator[%s]: %%33s, %%d", formatting.IntFormat(len(s.vdrSlice)-1))
	for i, vdr := range s.vdrSlice {
		sb.WriteString(fmt.Sprintf(format, i, vdr.ID(), vdr.Weight()))
	}

	return sb.String()
}
