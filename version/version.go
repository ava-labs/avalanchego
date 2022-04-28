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
	DefaultVersion1_0_0 = NewDefaultVersion(1, 0, 0)

	errDifferentMajor = errors.New("different major version")
)

type Version interface {
	fmt.Stringer

	Major() int
	Minor() int
	Patch() int
	// Compare returns a positive number if v > o, 0 if v == o, or a negative number if v < 0.
	Compare(o Version) int
}

type SemanticVersion struct {
	Major_  int    `yaml:"major"`
	Minor_  int    `yaml:"minor"`
	Patch_  int    `yaml:"patch"`
	String_ string `yaml:"string"`
}

func NewDefaultVersion(major, minor, patch int) Version {
	return NewVersion(major, minor, patch, defaultVersionPrefix, defaultVersionSeparator)
}

func NewVersion(major, minor, patch int, prefix, versionSeparator string) *SemanticVersion {
	return &SemanticVersion{
		Major_: major,
		Minor_: minor,
		Patch_: patch,
		String_: fmt.Sprintf(
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

func (v *SemanticVersion) String() string { return v.String_ }
func (v *SemanticVersion) Major() int     { return v.Major_ }
func (v *SemanticVersion) Minor() int     { return v.Minor_ }
func (v *SemanticVersion) Patch() int     { return v.Patch_ }

// Compare returns a positive number if v > o, 0 if v == o, or a negative number if v < 0.
func (v *SemanticVersion) Compare(o Version) int {
	{
		vm := v.Major()
		om := o.Major()

		if vm != om {
			return vm - om
		}
	}

	{
		vm := v.Minor()
		om := o.Minor()

		if vm != om {
			return vm - om
		}
	}

	{
		vp := v.Patch()
		op := o.Patch()

		return vp - op
	}
}
