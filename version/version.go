// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"sync"
)

var (
	// v1_0_0 is a useful version to use in tests
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

	initStrOnce sync.Once
	str         string
}

// The only difference here between Semantic and Application is that Semantic
// prepends "v" rather than "avalanche/".
func (s *Semantic) String() string {
	s.initStrOnce.Do(s.initString)
	return s.str
}

func (s *Semantic) initString() {
	s.str = fmt.Sprintf(
		"v%d.%d.%d",
		s.Major,
		s.Minor,
		s.Patch,
	)
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
