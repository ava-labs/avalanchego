// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/events"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var _ events.Blockable = (*rejector)(nil)

type rejector struct {
	g        *Directed
	errs     *wrappers.Errs
	deps     ids.Set
	rejected bool // true if the tx has been rejected
	txID     ids.ID
}

func (r *rejector) Dependencies() ids.Set { return r.deps }

func (r *rejector) Fulfill(context.Context, ids.ID) {
	if r.rejected || r.errs.Errored() {
		return
	}
	r.rejected = true
	asSet := ids.NewSet(1)
	asSet.Add(r.txID)
	r.errs.Add(r.g.reject(asSet))
}

func (*rejector) Abandon(context.Context, ids.ID) {}
func (*rejector) Update(context.Context)          {}
