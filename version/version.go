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

type version struct {
	major, minor, patch int
	str                 string
}

func NewDefaultVersion(major, minor, patch int) Version {
	return NewVersion(major, minor, patch, defaultVersionPrefix, defaultVersionSeparator)
}

func NewVersion(major, minor, patch int, prefix, versionSeparator string) Version {
	return &version{
		major: major,
		minor: minor,
		patch: patch,
		str: fmt.Sprintf(
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

func (v *version) String() string { return v.str }
func (v *version) Major() int     { return v.major }
func (v *version) Minor() int     { return v.minor }
func (v *version) Patch() int     { return v.patch }

// Compare returns a positive number if v > o, 0 if v == o, or a negative number if v < 0.
func (v *version) Compare(o Version) int {
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
