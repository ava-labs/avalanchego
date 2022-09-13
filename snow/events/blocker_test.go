// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

func TestBlocker(t *testing.T) {
	b := Blocker(nil)

	a := newTestBlockable()

	id0 := ids.GenerateTestID()
	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()

	calledDep := new(bool)
	a.dependencies = func() ids.Set {
		*calledDep = true

		s := ids.Set{}
		s.Add(id0, id1)
		return s
	}
	calledFill := new(bool)
	a.fulfill = func(ids.ID) {
		*calledFill = true
	}
	calledAbandon := new(bool)
	a.abandon = func(ids.ID) {
		*calledAbandon = true
	}
	calledUpdate := new(bool)
	a.update = func() {
		*calledUpdate = true
	}

	b.Register(a)

	switch {
	case !*calledDep, *calledFill, *calledAbandon, !*calledUpdate:
		t.Fatalf("Called wrong function")
	}

	b.Fulfill(id2)
	b.Abandon(id2)

	switch {
	case !*calledDep, *calledFill, *calledAbandon, !*calledUpdate:
		t.Fatalf("Called wrong function")
	}

	b.Fulfill(id0)

	switch {
	case !*calledDep, !*calledFill, *calledAbandon, !*calledUpdate:
		t.Fatalf("Called wrong function")
	}

	b.Abandon(id0)

	switch {
	case !*calledDep, !*calledFill, *calledAbandon, !*calledUpdate:
		t.Fatalf("Called wrong function")
	}

	b.Abandon(id1)

	switch {
	case !*calledDep, !*calledFill, !*calledAbandon, !*calledUpdate:
		t.Fatalf("Called wrong function")
	}
}

type testBlockable struct {
	dependencies func() ids.Set
	fulfill      func(ids.ID)
	abandon      func(ids.ID)
	update       func()
}

func newTestBlockable() *testBlockable {
	return &testBlockable{
		dependencies: func() ids.Set { return ids.Set{} },
		fulfill:      func(ids.ID) {},
		abandon:      func(ids.ID) {},
		update:       func() {},
	}
}

func (b *testBlockable) Dependencies() ids.Set { return b.dependencies() }
func (b *testBlockable) Fulfill(id ids.ID)     { b.fulfill(id) }
func (b *testBlockable) Abandon(id ids.ID)     { b.abandon(id) }
func (b *testBlockable) Update()               { b.update() }
