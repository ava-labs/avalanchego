// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

var _ json.Marshaler = (*Set[int])(nil)

// SampleableSet is a set of elements that supports sampling.
type SampleableSet[T comparable] struct {
	// indices maps the element in the set to the index that it appears in
	// elements.
	indices  map[T]int
	elements []T
}

// OfSampleable returns a Set initialized with [elts]
func OfSampleable[T comparable](elts ...T) SampleableSet[T] {
	s := NewSampleableSet[T](len(elts))
	s.Add(elts...)
	return s
}

// Return a new sampleable set with initial capacity [size].
// More or less than [size] elements can be added to this set.
// Using NewSampleableSet() rather than SampleableSet[T]{} is just an
// optimization that can be used if you know how many elements will be put in
// this set.
func NewSampleableSet[T comparable](size int) SampleableSet[T] {
	if size < 0 {
		return SampleableSet[T]{}
	}
	return SampleableSet[T]{
		indices:  make(map[T]int, size),
		elements: make([]T, 0, size),
	}
}

// Add all the elements to this set.
// If the element is already in the set, nothing happens.
func (s *SampleableSet[T]) Add(elements ...T) {
	s.resize(2 * len(elements))
	for _, e := range elements {
		s.add(e)
	}
}

// Union adds all the elements from the provided set to this set.
func (s *SampleableSet[T]) Union(set SampleableSet[T]) {
	s.resize(2 * set.Len())
	for _, e := range set.elements {
		s.add(e)
	}
}

// Difference removes all the elements in [set] from [s].
func (s *SampleableSet[T]) Difference(set SampleableSet[T]) {
	for _, e := range set.elements {
		s.remove(e)
	}
}

// Contains returns true iff the set contains this element.
func (s SampleableSet[T]) Contains(e T) bool {
	_, contains := s.indices[e]
	return contains
}

// Overlaps returns true if the intersection of the set is non-empty
func (s SampleableSet[T]) Overlaps(big SampleableSet[T]) bool {
	small := s
	if small.Len() > big.Len() {
		small, big = big, small
	}

	for _, e := range small.elements {
		if _, ok := big.indices[e]; ok {
			return true
		}
	}
	return false
}

// Len returns the number of elements in this set.
func (s SampleableSet[_]) Len() int {
	return len(s.elements)
}

// Remove all the given elements from this set.
// If an element isn't in the set, it's ignored.
func (s *SampleableSet[T]) Remove(elements ...T) {
	for _, e := range elements {
		s.remove(e)
	}
}

// Clear empties this set
func (s *SampleableSet[T]) Clear() {
	clear(s.indices)
	clear(s.elements)
	s.elements = s.elements[:0]
}

// List converts this set into a list
func (s SampleableSet[T]) List() []T {
	return slices.Clone(s.elements)
}

// Equals returns true if the sets contain the same elements
func (s SampleableSet[T]) Equals(other SampleableSet[T]) bool {
	if len(s.indices) != len(other.indices) {
		return false
	}
	for k := range s.indices {
		if _, ok := other.indices[k]; !ok {
			return false
		}
	}
	return true
}

func (s SampleableSet[T]) Sample(numToSample int) []T {
	if numToSample <= 0 {
		return nil
	}

	uniform := sampler.NewUniform()
	uniform.Initialize(uint64(len(s.elements)))
	indices, _ := uniform.Sample(min(len(s.elements), numToSample))
	elements := make([]T, len(indices))
	for i, index := range indices {
		elements[i] = s.elements[index]
	}
	return elements
}

func (s *SampleableSet[T]) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == avajson.Null {
		return nil
	}
	var elements []T
	if err := json.Unmarshal(b, &elements); err != nil {
		return err
	}
	s.Clear()
	s.Add(elements...)
	return nil
}

func (s *SampleableSet[_]) MarshalJSON() ([]byte, error) {
	var (
		elementBytes = make([][]byte, len(s.elements))
		err          error
	)
	for i, e := range s.elements {
		elementBytes[i], err = json.Marshal(e)
		if err != nil {
			return nil, err
		}
	}
	// Sort for determinism
	slices.SortFunc(elementBytes, bytes.Compare)

	// Build the JSON
	var (
		jsonBuf = bytes.Buffer{}
		errs    = wrappers.Errs{}
	)
	_, err = jsonBuf.WriteString("[")
	errs.Add(err)
	for i, elt := range elementBytes {
		_, err := jsonBuf.Write(elt)
		errs.Add(err)
		if i != len(elementBytes)-1 {
			_, err := jsonBuf.WriteString(",")
			errs.Add(err)
		}
	}
	_, err = jsonBuf.WriteString("]")
	errs.Add(err)

	return jsonBuf.Bytes(), errs.Err
}

func (s *SampleableSet[T]) resize(size int) {
	if s.elements == nil {
		if minSetSize > size {
			size = minSetSize
		}
		s.indices = make(map[T]int, size)
	}
}

func (s *SampleableSet[T]) add(e T) {
	_, ok := s.indices[e]
	if ok {
		return
	}

	s.indices[e] = len(s.elements)
	s.elements = append(s.elements, e)
}

func (s *SampleableSet[T]) remove(e T) {
	indexToRemove, ok := s.indices[e]
	if !ok {
		return
	}

	lastIndex := len(s.elements) - 1
	if indexToRemove != lastIndex {
		lastElement := s.elements[lastIndex]

		s.indices[lastElement] = indexToRemove
		s.elements[indexToRemove] = lastElement
	}

	delete(s.indices, e)
	s.elements[lastIndex] = utils.Zero[T]()
	s.elements = s.elements[:lastIndex]
}
