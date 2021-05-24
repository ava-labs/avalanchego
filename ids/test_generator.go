// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

var offset = uint64(0)

// GenerateTestID returns a new ID that should only be used for testing
func GenerateTestID() ID {
	offset++
	return Empty.Prefix(offset)
}

// GenerateTestShortID returns a new ID that should only be used for testing
func GenerateTestShortID() ShortID {
	newID := GenerateTestID()
	newShortID, _ := ToShortID(newID[:20])
	return newShortID
}
