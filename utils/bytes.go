// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

// CopyBytes returns a copy of the provided byte slice. If nil is provided, nil
// will be returned.
func CopyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}

	cb := make([]byte, len(b))
	copy(cb, b)
	return cb
}
