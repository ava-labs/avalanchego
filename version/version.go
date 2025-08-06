// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"sync/atomic"
)

var (
	// V1_0_0 is a useful version to use in tests
	Semantic1_0_0 = &Semantic{
		Major: 1,
		Minor: 0,
		Patch: 0,
	}

	_ fmt.Stringer = (*Semantic)(nil)
)

type Semantic struct {
	Major int `json:"major" yaml:"major"`
	Minor int `json:"minor" yaml:"minor"`
	Patch int `json:"patch" yaml:"patch"`

	str atomic.Value
}

// The only difference here between Semantic and Application is that Semantic
// prepends "v" rather than the client name.
func (s *Semantic) String() string {
	strIntf := s.str.Load()
	if strIntf != nil {
		return strIntf.(string)
	}

	str := fmt.Sprintf(
		"v%d.%d.%d",
		s.Major,
		s.Minor,
		s.Patch,
	)
	s.str.Store(str)
	return str
}

// Compare returns a positive number if s > o, 0 if s == o, or a negative number
// if s < o.
func (s *Semantic) Compare(o *Semantic) int {
	if s.Major != o.Major {
		return s.Major - o.Major
	}
	if s.Minor != o.Minor {
		return s.Minor - o.Minor
	}
	return s.Patch - o.Patch
}
