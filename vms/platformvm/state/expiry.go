// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	modified map[ExpiryEntry]bool // bool represents isAdded
	added    *btree.BTreeG[ExpiryEntry]
}

func newExpiryDiff() *expiryDiff {
	return &expiryDiff{
		modified: make(map[ExpiryEntry]bool),
		added:    btree.NewG(defaultTreeDegree, ExpiryEntry.Less),
	}
}

func (e *expiryDiff) PutExpiry(entry ExpiryEntry) {
	e.modified[entry] = true
	e.added.ReplaceOrInsert(entry)
}

func (e *expiryDiff) DeleteExpiry(entry ExpiryEntry) {
	e.modified[entry] = false
	e.added.Delete(entry)
}

func (e *expiryDiff) getExpiryIterator(parentIterator iterator.Iterator[ExpiryEntry]) iterator.Iterator[ExpiryEntry] {
	return iterator.Merge(
		ExpiryEntry.Less,
		iterator.Filter(parentIterator, func(entry ExpiryEntry) bool {
			_, ok := e.modified[entry]
			return ok
		}),
		iterator.FromTree(e.added),
	)
}
