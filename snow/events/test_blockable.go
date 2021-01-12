// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
	"github.com/ava-labs/avalanchego/ids"
)

type TestBlockable struct {
	DependenciesF      func() ids.Set
	DependenciesCalled bool
	FulfillF           func(ids.ID)
	FulfillCalled      bool
	AbandonF           func(ids.ID)
	AbandonCalled      bool
	UpdateF            func()
	UpdateCalled       bool
}

func (b *TestBlockable) Default() {
	*b = TestBlockable{
		DependenciesF: func() ids.Set {
			b.DependenciesCalled = true
			return ids.Set{}
		},
		FulfillF: func(ids.ID) { b.FulfillCalled = true },
		AbandonF: func(ids.ID) { b.AbandonCalled = true },
		UpdateF:  func() { b.UpdateCalled = true },
	}
}

func (b *TestBlockable) Dependencies() ids.Set { return b.DependenciesF() }
func (b *TestBlockable) Fulfill(id ids.ID)     { b.FulfillF(id) }
func (b *TestBlockable) Abandon(id ids.ID)     { b.AbandonF(id) }
func (b *TestBlockable) Update()               { b.UpdateF() }
