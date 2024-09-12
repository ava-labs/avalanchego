// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"fmt"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/iterator"
	"github.com/ava-labs/avalanchego/utils/set"
)

// expiryEntry = [timestamp] + [validationID]
const expiryEntryLength = database.Uint64Size + ids.IDLen

var (
	errUnexpectedExpiryEntryLength = fmt.Errorf("expected expiry entry length %d", expiryEntryLength)

	_ btree.LessFunc[ExpiryEntry] = ExpiryEntry.Less
	_ utils.Sortable[ExpiryEntry] = ExpiryEntry{}
)

type Expiry interface {
	// GetExpiryIterator returns an iterator of all the expiry entries in order
	// of lowest to highest timestamp.
	GetExpiryIterator() (iterator.Iterator[ExpiryEntry], error)

	// HasExpiry returns true if the database has the specified entry.
	HasExpiry(ExpiryEntry) (bool, error)

	// PutExpiry adds the entry to the database. If the entry already exists, it
	// is a noop.
	PutExpiry(ExpiryEntry)

	// DeleteExpiry removes the entry from the database. If the entry doesn't
	// exist, it is a noop.
	DeleteExpiry(ExpiryEntry)
}

type ExpiryEntry struct {
	Timestamp    uint64
	ValidationID ids.ID
}

func (e *ExpiryEntry) Marshal() []byte {
	data := make([]byte, expiryEntryLength)
	binary.BigEndian.PutUint64(data, e.Timestamp)
	copy(data[database.Uint64Size:], e.ValidationID[:])
	return data
}

func (e *ExpiryEntry) Unmarshal(data []byte) error {
	if len(data) != expiryEntryLength {
		return errUnexpectedExpiryEntryLength
	}

	e.Timestamp = binary.BigEndian.Uint64(data)
	copy(e.ValidationID[:], data[database.Uint64Size:])
	return nil
}

func (e ExpiryEntry) Less(o ExpiryEntry) bool {
	return e.Compare(o) == -1
}

// Invariant: Compare produces the same ordering as the marshalled bytes.
func (e ExpiryEntry) Compare(o ExpiryEntry) int {
	switch {
	case e.Timestamp < o.Timestamp:
		return -1
	case e.Timestamp > o.Timestamp:
		return 1
	default:
		return e.ValidationID.Compare(o.ValidationID)
	}
}

type expiryDiff struct {
	added   *btree.BTreeG[ExpiryEntry]
	removed set.Set[ExpiryEntry]
}

func newExpiryDiff() *expiryDiff {
	return &expiryDiff{
		added: btree.NewG(defaultTreeDegree, ExpiryEntry.Less),
	}
}

func (e *expiryDiff) PutExpiry(entry ExpiryEntry) {
	e.added.ReplaceOrInsert(entry)
	e.removed.Remove(entry)
}

func (e *expiryDiff) DeleteExpiry(entry ExpiryEntry) {
	e.added.Delete(entry)
	e.removed.Add(entry)
}

func (e *expiryDiff) getExpiryIterator(parentIterator iterator.Iterator[ExpiryEntry]) iterator.Iterator[ExpiryEntry] {
	// The iterators are deduplicated so that additions that were present in the
	// parent iterator are not duplicated.
	return iterator.Deduplicate(
		iterator.Filter(
			iterator.Merge(
				ExpiryEntry.Less,
				parentIterator,
				iterator.FromTree(e.added),
			),
			e.removed.Contains,
		),
	)
}

func (e *expiryDiff) hasExpiry(entry ExpiryEntry) (bool, bool) {
	switch {
	case e.removed.Contains(entry):
		return false, true
	case e.added.Has(entry):
		return true, true
	default:
		return false, false
	}
}
