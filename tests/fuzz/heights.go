// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var errForked = errors.New("honest nodes accepted different blocks at one height")

// agreement holds the longest chain of P-Chain blocks that the honest nodes
// accepted, and how far it compared each node's chain with it. The zero value
// is ready to use.
//
// Guarantee: Snowman never accepts two blocks at one height on honest nodes,
// so each honest node's accepted chain is a prefix of the others' (Stateful
// Fuzzing.md 3, agreement). Source: each node's consensus accept events, the
// block IDs that consensus hands its BlockAcceptor in order
// (snow/consensus/snowman/topological.go acceptPreferredChild). A node behind
// the others is compared as far as it got, and again once it catches up, so
// no fault excuses a fork.
type agreement struct {
	chain    []ids.ID    // chain[h-1] is the block at height h
	compared map[int]int // by node, how many of its blocks were compared
}

// add compares accepted, the blocks that node accepted in order, with the
// longest chain, and extends it. It returns errForked at the first height
// where they differ.
func (a *agreement) add(node int, accepted []ids.ID) error {
	if a.compared == nil {
		a.compared = make(map[int]int)
	}

	for h := a.compared[node]; h < len(accepted); h++ {
		switch {
		case h == len(a.chain):
			a.chain = append(a.chain, accepted[h])
		case a.chain[h] != accepted[h]:
			return fmt.Errorf("%w: node %d accepted %s at height %d, and another node accepted %s", errForked, node, accepted[h], h+1, a.chain[h])
		}
		a.compared[node] = h + 1
	}

	return nil
}

// heldT records the first failure of the checks that read one node, so that
// it counts only once the node is known to have stayed at the height that the
// checks judge (atHeight). It is a [require.TestingT], so the postconditions
// fail through it as through a test.
type heldT struct {
	check string // the check that failed; "postcondition" for a failed require
	err   error
}

// errHeld stops the reads after a failure, as FailNow stops a test.
var errHeld = errors.New("held failure")

// Errorf records the first failure, as a postcondition.
func (h *heldT) Errorf(format string, args ...any) {
	if h.err == nil {
		h.check, h.err = "postcondition", fmt.Errorf(format, args...)
	}
}

// FailNow stops the reads. atHeight recovers it.
func (*heldT) FailNow() {
	panic(errHeld)
}

// fail records that check failed with err, and stops the reads.
func (h *heldT) fail(check string, err error) {
	if h.err == nil {
		h.check, h.err = check, err
	}
	h.FailNow()
}

// atHeight runs read if height, a node's accepted height, reports h, and
// reports whether it still reports h after read. Heights only grow, so only
// then were all of read's reads at h. read judges what it reads through its
// heldT, and atHeight returns that, which counts only if the node stayed. err
// is an error of height itself.
func atHeight(h uint64, height func() (uint64, error), read func(*heldT)) (held heldT, stayed bool, err error) {
	before, err := height()
	if err != nil || before != h {
		return heldT{}, false, err
	}

	func() {
		defer func() {
			if p := recover(); p != nil && p != errHeld { //nolint:errorlint // the sentinel is panicked as is
				panic(p)
			}
		}()
		read(&held)
	}()

	after, err := height()
	if err != nil || after != h {
		return heldT{}, false, err
	}

	return held, true, nil
}
