// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package event

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestBlocker(t *testing.T) {
	require := require.New(t)

	b := Blocker(nil)

	a := newTestBlockable()

	id0 := ids.GenerateTestID()
	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()

	calledDep := new(bool)
	a.dependencies = func() set.Set[ids.ID] {
		*calledDep = true

		s := set.Of(id0, id1)
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

	require.True(*calledDep)
	require.False(*calledFill)
	require.False(*calledAbandon)
	require.True(*calledUpdate)

	b.Fulfill(context.Background(), id2)
	b.Abandon(context.Background(), id2)

	require.True(*calledDep)
	require.False(*calledFill)
	require.False(*calledAbandon)
	require.True(*calledUpdate)

	b.Fulfill(context.Background(), id0)

	require.True(*calledDep)
	require.True(*calledFill)
	require.False(*calledAbandon)
	require.True(*calledUpdate)

	b.Abandon(context.Background(), id0)

	require.True(*calledDep)
	require.True(*calledFill)
	require.False(*calledAbandon)
	require.True(*calledUpdate)

	b.Abandon(context.Background(), id1)

	require.True(*calledDep)
	require.True(*calledFill)
	require.True(*calledAbandon)
	require.True(*calledUpdate)
}

type testBlockable struct {
	dependencies func() set.Set[ids.ID]
	fulfill      func(context.Context, ids.ID)
	abandon      func(context.Context, ids.ID)
	update       func(context.Context)
}

func newTestBlockable() *testBlockable {
	return &testBlockable{
		dependencies: func() set.Set[ids.ID] {
			return set.Set[ids.ID]{}
		},
		fulfill: func(context.Context, ids.ID) {},
		abandon: func(context.Context, ids.ID) {},
		update:  func(context.Context) {},
	}
}

func (b *testBlockable) Dependencies() set.Set[ids.ID] {
	return b.dependencies()
}

func (b *testBlockable) Fulfill(ctx context.Context, id ids.ID) {
	b.fulfill(ctx, id)
}

func (b *testBlockable) Abandon(ctx context.Context, id ids.ID) {
	b.abandon(ctx, id)
}

func (b *testBlockable) Update(ctx context.Context) {
	b.update(ctx)
}
