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
	errDifferentApps = errors.New("different applications")
)

type Application struct {
	Semantic
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
	v := NewSemantic(major, minor, patch, "", versionSeparator)
	return Application{
		Semantic: v,
		app:      app,
		str: fmt.Sprintf("%s%s%s",
			app,
			appSeparator,
			v,
		),
	}
}

func (a Application) App() string    { return a.app }
func (a Application) String() string { return a.str }

func (a Application) Compatible(other Application) error {
	switch {
	case a.App() != other.App():
		return errDifferentApps
	case a.Major > other.Major:
		return errDifferentMajor
	default:
		return nil
	}
}

func (a Application) Before(other Application) bool {
	if a.App() != other.App() {
		return false
	}

	return a.Compare(other.Semantic) < 0
}
