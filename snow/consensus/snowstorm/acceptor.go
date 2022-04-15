// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/events"
	"github.com/chain4travel/caminogo/utils/wrappers"
)

var _ events.Blockable = &acceptor{}

type acceptor struct {
	g        *Directed
	errs     *wrappers.Errs
	deps     ids.Set
	rejected bool
	txID     ids.ID
}

func (a *acceptor) Dependencies() ids.Set { return a.deps }

func (a *acceptor) Fulfill(id ids.ID) {
	a.deps.Remove(id)
	a.Update()
}

func (a *acceptor) Abandon(id ids.ID) { a.rejected = true }

func (a *acceptor) Update() {
	// If I was rejected or I am still waiting on dependencies to finish or an
	// error has occurred, I shouldn't do anything.
	if a.rejected || a.deps.Len() != 0 || a.errs.Errored() {
		return
	}
	a.errs.Add(a.g.accept(a.txID))
}
