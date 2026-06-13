// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

package ffi

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type kvIter interface {
	SetBatchSize(int)
	Next() bool
	Key() []byte
	Value() []byte
	Err() error
	Drop() error
}
type borrowIter struct{ it *Iterator }

func (b *borrowIter) SetBatchSize(batchSize int) { b.it.SetBatchSize(batchSize) }
func (b *borrowIter) Next() bool                 { return b.it.NextBorrowed() }
func (b *borrowIter) Key() []byte                { return b.it.Key() }
func (b *borrowIter) Value() []byte              { return b.it.Value() }
func (b *borrowIter) Err() error                 { return b.it.Err() }
func (b *borrowIter) Drop() error                { return b.it.Drop() }

func assertIteratorYields(r *require.Assertions, it kvIter, keys [][]byte, vals [][]byte) {
	i := 0
	for ; it.Next(); i += 1 {
		r.Equal(keys[i], it.Key())
		r.Equal(vals[i], it.Value())
	}
	r.NoError(it.Err())
	r.Equal(len(keys), i)
}

type iteratorConfigFn = func(it kvIter) kvIter

var iterConfigs = map[string]iteratorConfigFn{
	"Owned":    func(it kvIter) kvIter { return it },
	"Borrowed": func(it kvIter) kvIter { return &borrowIter{it: it.(*Iterator)} },
	"Single": func(it kvIter) kvIter {
		it.SetBatchSize(1)
		return it
	},
	"Batched": func(it kvIter) kvIter {
		it.SetBatchSize(100)
		return it
	},
}

func runIteratorTestForModes(t *testing.T, fn func(*testing.T, iteratorConfigFn), modes ...string) {
	testName := strings.Join(modes, "/")
	t.Run(testName, func(t *testing.T) {
		r := require.New(t)
		fn(t, func(it kvIter) kvIter {
			for _, m := range modes {
				config, ok := iterConfigs[m]
				r.Truef(ok, "specified config mode %s does not exist", m)
				it = config(it)
			}
			return it
		})
	})
}

func runIteratorTestForAllModes(parentT *testing.T, fn func(*testing.T, iteratorConfigFn)) {
	for _, dataMode := range []string{"Owned", "Borrowed"} {
		for _, batchMode := range []string{"Single", "Batched"} {
			runIteratorTestForModes(parentT, fn, batchMode, dataMode)
		}
	}
}

// Tests that basic iterator functionality works
func TestIter(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals, batch := kvForTest(100)
	_, err := db.Update(batch)
	r.NoError(err)

	runIteratorTestForAllModes(t, func(t *testing.T, cfn iteratorConfigFn) {
		r := require.New(t)
		rev, err := db.LatestRevision()
		r.NoError(err)
		it, err := rev.Iter(nil)
		r.NoError(err)
		t.Cleanup(func() {
			r.NoError(it.Drop())
			r.NoError(rev.Drop())
		})

		assertIteratorYields(r, cfn(it), keys, vals)
	})
}

func TestIterOnRoot(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals, batch := kvForTest(240)
	firstRoot, err := db.Update(batch[:80])
	r.NoError(err)
	secondRoot, err := db.Update(batch[80:160])
	r.NoError(err)
	thirdRoot, err := db.Update(batch[160:])
	r.NoError(err)

	runIteratorTestForAllModes(t, func(t *testing.T, cfn iteratorConfigFn) {
		r := require.New(t)
		r1, err := db.Revision(firstRoot)
		r.NoError(err)
		h1, err := r1.Iter(nil)
		r.NoError(err)
		t.Cleanup(func() {
			r.NoError(h1.Drop())
			r.NoError(r1.Drop())
		})

		r2, err := db.Revision(secondRoot)
		r.NoError(err)
		h2, err := r2.Iter(nil)
		r.NoError(err)
		t.Cleanup(func() {
			r.NoError(h2.Drop())
			r.NoError(r2.Drop())
		})

		r3, err := db.Revision(thirdRoot)
		r.NoError(err)
		h3, err := r3.Iter(nil)
		r.NoError(err)
		t.Cleanup(func() {
			r.NoError(h3.Drop())
			r.NoError(r3.Drop())
		})

		assertIteratorYields(r, cfn(h1), keys[:80], vals[:80])
		assertIteratorYields(r, cfn(h2), keys[:160], vals[:160])
		assertIteratorYields(r, cfn(h3), keys, vals)
	})
}

func TestIterOnProposal(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)
	keys, vals, batch := kvForTest(240)
	_, err := db.Update(batch)
	r.NoError(err)

	runIteratorTestForAllModes(t, func(t *testing.T, cfn iteratorConfigFn) {
		r := require.New(t)
		updatedValues := make([][]byte, len(vals))
		copy(updatedValues, vals)

		changedKeys := make([][]byte, 0)
		changedVals := make([][]byte, 0)
		for i := 0; i < len(vals); i += 4 {
			changedKeys = append(changedKeys, keys[i])
			newVal := []byte{byte(i)}
			changedVals = append(changedVals, newVal)
			updatedValues[i] = newVal
		}
		p, err := db.Propose(makeBatch(changedKeys, changedVals))
		r.NoError(err)
		it, err := p.Iter(nil)
		r.NoError(err)
		t.Cleanup(func() {
			r.NoError(it.Drop())
		})

		assertIteratorYields(r, cfn(it), keys, updatedValues)
	})
}

// Tests that the iterator still works after proposal is committed
func TestIterAfterProposalCommit(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys, vals, batch := kvForTest(10)
	p, err := db.Propose(batch)
	r.NoError(err)

	it, err := p.Iter(nil)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(it.Drop())
	})

	err = p.Commit()
	r.NoError(err)

	// iterate after commit
	// because iterator hangs on the nodestore reference of proposal
	// the nodestore won't be dropped until we drop the iterator
	assertIteratorYields(r, it, keys, vals)
}

// Tests that the iterator on latest revision works properly after a proposal commit
func TestIterUpdate(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys, vals, batch := kvForTest(10)
	_, err := db.Update(batch)
	r.NoError(err)

	// get an iterator on latest revision
	rev, err := db.LatestRevision()
	r.NoError(err)
	it, err := rev.Iter(nil)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(it.Drop())
		r.NoError(rev.Drop())
	})

	// update the database
	_, _, batch2 := kvForTest(10)
	_, err = db.Update(batch2)
	r.NoError(err)

	// iterate after commit
	// because iterator is fixed on the revision hash, it should return the initial values
	assertIteratorYields(r, it, keys, vals)
}

// Tests the iterator's behavior after exhaustion, should safely return empty item/batch, indicating done
func TestIterDone(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys, vals, batch := kvForTest(18)
	_, err := db.Update(batch)
	r.NoError(err)

	// get an iterator on latest revision
	rev, err := db.LatestRevision()
	r.NoError(err)
	it, err := rev.Iter(nil)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(it.Drop())
		r.NoError(rev.Drop())
	})
	// consume the iterator
	assertIteratorYields(r, it, keys, vals)
	// calling next again should be safe and return false
	r.False(it.Next())
	r.NoError(it.Err())

	// get a new iterator
	it2, err := rev.Iter(nil)
	t.Cleanup(func() {
		r.NoError(it2.Drop())
	})
	r.NoError(err)
	// set batch size to 5
	it2.SetBatchSize(5)
	// consume the iterator
	assertIteratorYields(r, it2, keys, vals)
	// calling next again should be safe and return false
	r.False(it.Next())
	r.NoError(it.Err())
	r.NoError(it2.Drop())
}

// Tests the iterator's behavior after revision is dropped, should safely
// outlive the revision as it owns a reference to the revision in Rust.
func TestIterOutlivesRevision(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t, func(config *config) {
		config.revisions = 2
	})

	keys, vals, batch := kvForTest(30)
	_, err := db.Update(batch[:10])
	r.NoErrorf(err, "%T.Update(...)", db)
	rev, err := db.LatestRevision()
	r.NoErrorf(err, "%T.LatestRevision()", db)
	it, err := rev.Iter(nil)
	r.NoErrorf(err, "%T.Iter()", rev)
	t.Cleanup(func() {
		r.NoErrorf(it.Drop(), "%T.Drop()", it)
	})

	// Drop the revision right away to ensure reaping
	r.NoErrorf(rev.Drop(), "%T.Drop()", rev)

	// Commit two more times to force reaping of the first revision
	_, err = db.Update(batch[10:20])
	r.NoErrorf(err, "%T.Update(...)", db)
	_, err = db.Update(batch[20:])
	r.NoErrorf(err, "%T.Update(...)", db)

	// iterate after reaping
	assertIteratorYields(r, it, keys[:10], vals[:10])
}

// Tests the iterator's behavior after proposal is dropped, should safely
// outlive the proposal as it owns a reference to the view in Rust.
func TestIterOutlivesProposal(t *testing.T) {
	r := require.New(t)
	db := newTestDatabase(t)

	keys, vals, batch := kvForTest(4)
	p, err := db.Propose(batch[:2])
	r.NoErrorf(err, "%T.Propose(...)", db)

	it, err := p.Iter(nil)
	r.NoErrorf(err, "%T.Iter()", p)
	t.Cleanup(func() {
		r.NoErrorf(it.Drop(), "%T.Drop()", it)
	})
	r.NoErrorf(p.Drop(), "%T.Drop()", p)

	// This is necessary because the dropped proposal is referenced in
	// the revision manager and cleanup only runs when commit is called.
	// So here we create a new proposal with different keys, and commit
	// to trigger cleanup of the dropped proposal.
	p2, err := db.Propose(batch[2:])
	r.NoErrorf(err, "%T.Propose(...)", db)
	r.NoErrorf(p2.Commit(), "%T.Commit(...)", db)

	assertIteratorYields(r, it, keys[:2], vals[:2])
}
