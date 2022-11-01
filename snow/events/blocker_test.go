// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
	"context"
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
	a.fulfill = func(context.Context, ids.ID) {
		*calledFill = true
	}
	calledAbandon := new(bool)
	a.abandon = func(context.Context, ids.ID) {
		*calledAbandon = true
	}
	calledUpdate := new(bool)
	a.update = func(context.Context) {
		*calledUpdate = true
	}

	b.Register(context.Background(), a)

	switch {
	case !*calledDep, *calledFill, *calledAbandon, !*calledUpdate:
		t.Fatalf("Called wrong function")
	}

	b.Fulfill(context.Background(), id2)
	b.Abandon(context.Background(), id2)

	switch {
	case !*calledDep, *calledFill, *calledAbandon, !*calledUpdate:
		t.Fatalf("Called wrong function")
	}

	b.Fulfill(context.Background(), id0)

	switch {
	case !*calledDep, !*calledFill, *calledAbandon, !*calledUpdate:
		t.Fatalf("Called wrong function")
	}

	b.Abandon(context.Background(), id0)

	switch {
	case !*calledDep, !*calledFill, *calledAbandon, !*calledUpdate:
		t.Fatalf("Called wrong function")
	}

	b.Abandon(context.Background(), id1)

	switch {
	case !*calledDep, !*calledFill, !*calledAbandon, !*calledUpdate:
		t.Fatalf("Called wrong function")
	}
}

type testBlockable struct {
	dependencies func() ids.Set
	fulfill      func(context.Context, ids.ID)
	abandon      func(context.Context, ids.ID)
	update       func(context.Context)
}

func newTestBlockable() *testBlockable {
	return &testBlockable{
		dependencies: func() ids.Set { return ids.Set{} },
		fulfill:      func(context.Context, ids.ID) {},
		abandon:      func(context.Context, ids.ID) {},
		update:       func(context.Context) {},
	}
}

func (b *testBlockable) Dependencies() ids.Set                  { return b.dependencies() }
func (b *testBlockable) Fulfill(ctx context.Context, id ids.ID) { b.fulfill(ctx, id) }
func (b *testBlockable) Abandon(ctx context.Context, id ids.ID) { b.abandon(ctx, id) }
func (b *testBlockable) Update(ctx context.Context)             { b.update(ctx) }
