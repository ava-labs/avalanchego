// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"cmp"
	"fmt"
	"sync"
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

	makeStrOnce sync.Once
	str         string
}

// The only difference here between Semantic and Application is that Semantic
// prepends "v" rather than the client name.
func (s *Semantic) String() string {
	s.makeStrOnce.Do(s.initString)
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

// Compare returns
//
//	-1 if s is less than o,
//	 0 if s equals o,
//	+1 if s is greater than o.
func (s *Semantic) Compare(o *Application) int {
	if c := cmp.Compare(s.Major, o.Major); c != 0 {
		return c
	}
	if c := cmp.Compare(s.Minor, o.Minor); c != 0 {
		return c
	}
	return cmp.Compare(s.Patch, o.Patch)
}
