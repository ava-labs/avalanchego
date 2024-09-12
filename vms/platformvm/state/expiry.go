// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"fmt"

	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

// expiryEntry = [timestamp] + [validationID]
const expiryEntryLength = database.Uint64Size + ids.IDLen

var (
	errUnexpectedExpiryEntryLength = fmt.Errorf("expected expiry entry length %d", expiryEntryLength)

	_ btree.LessFunc[ExpiryEntry] = ExpiryEntry.Less
)

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

// Invariant: Less produces the same ordering as the marshalled bytes.
func (e ExpiryEntry) Less(o ExpiryEntry) bool {
	switch {
	case e.Timestamp < o.Timestamp:
		return true
	case e.Timestamp > o.Timestamp:
		return false
	default:
		return e.ValidationID.Compare(o.ValidationID) == -1
	}
}
