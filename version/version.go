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
)

var (
	errDifferentApps  = errors.New("different applications")
	errDifferentMajor = errors.New("different major version")
)

// Version defines what is needed to describe a version
type Version interface {
	fmt.Stringer

	App() string
	Major() int
	Minor() int
	Patch() int

	Compatible(Version) error
	Before(Version) bool
}

type version struct {
	app   string
	major int
	minor int
	patch int
	str   string
}

// NewDefaultVersion returns a new version with default separators
func NewDefaultVersion(
	app string,
	major int,
	minor int,
	patch int,
) Version {
	return NewVersion(
		app,
		defaultAppSeparator,
		defaultVersionSeparator,
		major,
		minor,
		patch,
	)
}

// NewVersion returns a new version
func NewVersion(
	app string,
	appSeparator string,
	versionSeparator string,
	major int,
	minor int,
	patch int,
) Version {
	return &version{
		app:   app,
		major: major,
		minor: minor,
		patch: patch,
		str: fmt.Sprintf("%s%s%d%s%d%s%d",
			app,
			appSeparator,
			major,
			versionSeparator,
			minor,
			versionSeparator,
			patch,
		),
	}
}

func (v *version) App() string    { return v.app }
func (v *version) Major() int     { return v.major }
func (v *version) Minor() int     { return v.minor }
func (v *version) Patch() int     { return v.patch }
func (v *version) String() string { return v.str }

func (v *version) Compatible(o Version) error {
	switch {
	case v.App() != o.App():
		return errDifferentApps
	case v.Major() > o.Major():
		return errDifferentMajor
	default:
		return nil
	}
}

func (v *version) Before(o Version) bool {
	if v.App() != o.App() {
		return false
	}

	{
		v := v.Major()
		o := o.Major()
		switch {
		case v < o:
			return true
		case v > o:
			return false
		}
	}

	{
		v := v.Minor()
		o := o.Minor()
		switch {
		case v < o:
			return true
		case v > o:
			return false
		}
	}

	{
		v := v.Patch()
		o := o.Patch()
		if v < o {
			return true
		}
	}

	return false
}
