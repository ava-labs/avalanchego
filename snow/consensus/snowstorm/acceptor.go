// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/events"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var _ events.Blockable = (*acceptor)(nil)

type acceptor struct {
	g        *Directed
	errs     *wrappers.Errs
	deps     set.Set[ids.ID]
	rejected bool
	txID     ids.ID
}

func (a *acceptor) Dependencies() set.Set[ids.ID] {
	return a.deps
}

func (a *acceptor) Fulfill(ctx context.Context, id ids.ID) {
	a.deps.Remove(id)
	a.Update(ctx)
}

func (a *acceptor) Abandon(context.Context, ids.ID) {
	a.rejected = true
}

func (a *acceptor) Update(ctx context.Context) {
	// If I was rejected or I am still waiting on dependencies to finish or an
	// error has occurred, I shouldn't do anything.
	if a.rejected || a.deps.Len() != 0 || a.errs.Errored() {
		return
	}
	a.errs.Add(a.g.accept(ctx, a.txID))
}
