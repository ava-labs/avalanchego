// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"errors"
	"fmt"
)

const (
	defaultAppSeparator = "/"
)

var (
	errDifferentApps             = errors.New("different applications")
	_                Application = &application{}
)

// Application defines what is needed to describe a versioned
// Application.
type Application interface {
	Version
	App() string
	Compatible(Application) error
	Before(Application) bool
}

type application struct {
	Version
	app string
	str string
}

// NewDefaultApplication returns a new version with default separators
func NewDefaultApplication(
	app string,
	major int,
	minor int,
	patch int,
) Application {
	return NewApplication(
		app,
		defaultAppSeparator,
		defaultVersionSeparator,
		major,
		minor,
		patch,
	)
}

// NewApplication returns a new version
func NewApplication(
	app string,
	appSeparator string,
	versionSeparator string,
	major int,
	minor int,
	patch int,
) Application {
	v := NewVersion(major, minor, patch, "", versionSeparator)
	return &application{
		Version: v,
		app:     app,
		str: fmt.Sprintf("%s%s%s",
			app,
			appSeparator,
			v,
		),
	}
}

func (v *application) App() string    { return v.app }
func (v *application) String() string { return v.str }

func (v *application) Compatible(o Application) error {
	switch {
	case v.App() != o.App():
		return errDifferentApps
	case v.Major() > o.Major():
		return errDifferentMajor
	default:
		return nil
	}
}

func (v *application) Before(o Application) bool {
	if v.App() != o.App() {
		return false
	}

	return v.Compare(o) < 0
}
