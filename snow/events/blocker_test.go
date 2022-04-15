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

package events

import (
	"testing"

	"github.com/chain4travel/caminogo/ids"
)

func TestBlocker(t *testing.T) {
	b := Blocker(nil)

	a := &blockable{}
	a.Default()

	id0 := GenerateID()
	id1 := GenerateID()
	id2 := GenerateID()

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
