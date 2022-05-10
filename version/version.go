// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"errors"
	"fmt"
)

const (
	defaultVersionSeparator = "."
	defaultVersionPrefix    = "v"
)

var (
	// DefaultVersion1_0_0 is a useful version to use in tests
	DefaultVersion1_0_0 = NewDefaultSemantic(1, 0, 0)

	errDifferentMajor = errors.New("different major version")

	_ fmt.Stringer = &Semantic{}
)

type Semantic struct {
	Major int    `json:"major" yaml:"major"`
	Minor int    `json:"minor" yaml:"minor"`
	Patch int    `json:"patch" yaml:"patch"`
	Str   string `json:"string" yaml:"string"`
}

func NewDefaultSemantic(major, minor, patch int) Semantic {
	return NewSemantic(major, minor, patch, defaultVersionPrefix, defaultVersionSeparator)
}

func NewSemantic(major, minor, patch int, prefix, versionSeparator string) Semantic {
	return Semantic{
		Major: major,
		Minor: minor,
		Patch: patch,
		Str: fmt.Sprintf(
			"%s%d%s%d%s%d",
			prefix,
			major,
			versionSeparator,
			minor,
			versionSeparator,
			patch,
		),
	}
}

func (s Semantic) String() string { return s.Str }

// Compare returns a positive number if v > o, 0 if v == o, or a negative number if v < 0.
func (s Semantic) Compare(other Semantic) int {
	{
		vm := s.Major
		om := other.Major

		if vm != om {
			return vm - om
		}
	}

	{
		vm := s.Minor
		om := other.Minor

		if vm != om {
			return vm - om
		}
	}

	{
		vp := s.Patch
		op := other.Patch

		return vp - op
	}
}
