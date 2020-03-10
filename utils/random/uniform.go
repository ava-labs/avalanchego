// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random

type defaultMap map[int]int

func (m *defaultMap) get(key int, def int) int {
	if *m == nil {
		*m = make(defaultMap)
	}
	if r, ok := (*m)[key]; ok {
		return r
	}
	return def
}

// Uniform implements the Sampler interface by using the uniform distribution in
// the range [0, N). All operations run in O(1) time.
type Uniform struct {
	drawn defaultMap
	N, i  int
}

// Sample implements the Sampler interface
func (s *Uniform) Sample() int {
	r := Rand(s.i, s.N)

	ret := s.drawn.get(r, r)
	s.drawn[r] = s.drawn.get(s.i, s.i)

	s.i++
	return ret
}

// SampleReplace implements the Sampler interface
func (s *Uniform) SampleReplace() int {
	r := Rand(s.i, s.N)
	return s.drawn.get(r, r)
}

// CanSample implements the Sampler interface
func (s *Uniform) CanSample() bool { return s.i < s.N }

// Replace implements the Sampler interface
func (s *Uniform) Replace() {
	s.drawn = make(defaultMap)
	s.i = 0
}
