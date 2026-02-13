// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"cmp"
	"fmt"
	"sync"
)

var _ fmt.Stringer = (*Application)(nil)

type Application struct {
	Name  string `json:"name"  yaml:"name"`
	Major int    `json:"major" yaml:"major"`
	Minor int    `json:"minor" yaml:"minor"`
	Patch int    `json:"patch" yaml:"patch"`

	makeStrOnce sync.Once
	str         string
}

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

// Semantic returns the semantic version string (e.g., "v1.14.1")
func (a *Application) Semantic() string {
	return fmt.Sprintf("v%d.%d.%d", a.Major, a.Minor, a.Patch)
}

// SemanticWithCommit returns the semantic version string with an optional git commit suffix
func (a *Application) SemanticWithCommit(gitCommit string) string {
	v := a.Semantic()
	if len(gitCommit) != 0 {
		return fmt.Sprintf("%s@%s", v, gitCommit)
	}
	return v
}

// Compare returns
//
//	-1 if a is less than o,
//	 0 if a equals o,
//	+1 if a is greater than o.
func (a *Application) Compare(o *Application) int {
	if c := cmp.Compare(a.Major, o.Major); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Minor, o.Minor); c != 0 {
		return c
	}
	return cmp.Compare(a.Patch, o.Patch)
}
