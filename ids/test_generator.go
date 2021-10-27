// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import "go.uber.org/atomic"

var offset = atomic.NewUint64(0)

// GenerateTestID returns a new ID that should only be used for testing
func GenerateTestID() ID {
	return Empty.Prefix(offset.Inc())
}

// GenerateTestShortID returns a new ID that should only be used for testing
func GenerateTestShortID() ShortID {
	newID := GenerateTestID()
	newShortID, _ := ToShortID(newID[:20])
	return newShortID
}
