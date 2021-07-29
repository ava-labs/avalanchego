// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"crypto/rand"
	"io"
)

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

// RandomBytes returns a slice of n random bytes
// Intended for use in testing
func RandomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// ReadAtMost is adapted from io.ReadFull
func ReadAtMost(r io.Reader, maxSize int) ([]byte, error) {
	b := make([]byte, 0, 512)
	for {
		if len(b) == cap(b) {
			// Add more capacity (let append pick how much).
			b = append(b, 0)[:len(b)]
		}

		limit := cap(b)
		if limit > maxSize {
			limit = maxSize
		}

		n, err := r.Read(b[len(b):limit])
		b = b[:len(b)+n]
		if err == io.EOF || len(b) >= maxSize {
			return b, nil
		}
		if err != nil {
			return b, err
		}
	}
}
