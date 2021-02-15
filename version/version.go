// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"errors"
	"fmt"
)

const (
	defaultAppSeparator     = "/"
	defaultVersionSeparator = "."
	defaultVersionPrefix    = "v"
)

var (
	errDifferentApps  = errors.New("different applications")
	errDifferentMajor = errors.New("different major version")
)

type Version interface {
	fmt.Stringer

	Major() int
	Minor() int
	Patch() int
	Compare(v Version) int
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

func (v *version) Compare(o Version) int {
	var (
		vm, om int
	)
	{
		vm = v.Major()
		om = o.Major()

		if vm != om {
			return vm - om
		}
	}

	{
		vm = v.Minor()
		om = o.Minor()

		if vm != om {
			return vm - om
		}
	}

	{
		vm = v.Patch()
		om = o.Patch()

		return vm - om
	}
}

// ApplicationVersion defines what is needed to describe a version
type ApplicationVersion interface {
	Version

	App() string

	Compatible(ApplicationVersion) error
	Before(ApplicationVersion) bool
}

type appVersion struct {
	Version

	app string
	str string
}

// NewDefaultApplicationVersion returns a new version with default separators
func NewDefaultApplicationVersion(
	app string,
	major int,
	minor int,
	patch int,
) ApplicationVersion {
	return NewApplicationVersion(
		app,
		defaultAppSeparator,
		defaultVersionSeparator,
		major,
		minor,
		patch,
	)
}

// NewApplicationVersion returns a new version
func NewApplicationVersion(
	app string,
	appSeparator string,
	versionSeparator string,
	major int,
	minor int,
	patch int,
) ApplicationVersion {
	v := NewVersion(major, minor, patch, "", versionSeparator)
	return &appVersion{
		app:     app,
		Version: v,
		str: fmt.Sprintf("%s%s%s",
			app,
			appSeparator,
			v.String(),
		),
	}
}

func (v *appVersion) App() string    { return v.app }
func (v *appVersion) String() string { return v.str }

func (v *appVersion) Compatible(o ApplicationVersion) error {
	switch {
	case v.App() != o.App():
		return errDifferentApps
	case v.Major() > o.Major():
		return errDifferentMajor
	default:
		return nil
	}
}

func (v *appVersion) Before(o ApplicationVersion) bool {
	if v.App() != o.App() {
		return false
	}

	return v.Compare(o) < 0
}
