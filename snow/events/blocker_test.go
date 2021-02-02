// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

func TestBlocker(t *testing.T) {
	b := Blocker(nil)

	a := &TestBlockable{}
	a.Default()

	id0 := ids.GenerateTestID()
	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()

	calledDep := new(bool)
	a.DependenciesF = func() ids.Set {
		*calledDep = true

		s := ids.Set{}
		s.Add(id0, id1)
		return s
	}
	calledFill := new(bool)
	a.FulfillF = func(ids.ID) {
		*calledFill = true
	}
	calledAbandon := new(bool)
	a.AbandonF = func(ids.ID) {
		*calledAbandon = true
	}
	calledUpdate := new(bool)
	a.UpdateF = func() {
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
