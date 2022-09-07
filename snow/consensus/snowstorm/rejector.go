// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/events"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var _ events.Blockable = &rejector{}

type rejector struct {
	g        *Directed
	errs     *wrappers.Errs
	deps     set.Set[ids.ID]
	rejected bool // true if the tx has been rejected
	txID     ids.ID
}

func (r *rejector) Dependencies() set.Set[ids.ID] { return r.deps }

func (r *rejector) Fulfill(ids.ID) {
	if r.rejected || r.errs.Errored() {
		return
	}
	r.rejected = true
	asSet := set.NewSet[ids.ID](1)
	asSet.Add(r.txID)
	r.errs.Add(r.g.reject(asSet))
}

func (*rejector) Abandon(ids.ID) {}
func (*rejector) Update()        {}
