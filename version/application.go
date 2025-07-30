// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"errors"
	"fmt"
	"sync"
)

var (
	errDifferentMajor = errors.New("different major version")

	_ fmt.Stringer = (*Application)(nil)
)

type Application struct {
	Name  string `json:"name"  yaml:"name"`
	Major int    `json:"major" yaml:"major"`
	Minor int    `json:"minor" yaml:"minor"`
	Patch int    `json:"patch" yaml:"patch"`

	makeStrOnce sync.Once
	str         string
}

// The only difference here between Application and Semantic is that Application
// prepends the client name rather than "v".
func (a *Application) String() string {
	a.makeStrOnce.Do(a.initString)
	return a.str
}

func (a *Application) initString() {
	a.str = fmt.Sprintf(
		"%s/%d.%d.%d",
		a.Name,
		a.Major,
		a.Minor,
		a.Patch,
	)
}

func (a *Application) Compatible(o *Application) error {
	switch {
	case a.Major > o.Major:
		return errDifferentMajor
	default:
		return nil
	}
}

func (a *Application) Before(o *Application) bool {
	return a.Compare(o) < 0
}

// Compare returns a positive number if s > o, 0 if s == o, or a negative number
// if s < o.
func (a *Application) Compare(o *Application) int {
	if a.Major != o.Major {
		return a.Major - o.Major
	}
	if a.Minor != o.Minor {
		return a.Minor - o.Minor
	}
	return a.Patch - o.Patch
}
