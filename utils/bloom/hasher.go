// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"crypto/sha256"
	"encoding/binary"
)

func Add(f *Filter, key, salt []byte) {
	f.Add(Hash(key, salt))
}

func Contains(c Checker, key, salt []byte) bool {
	return c.Contains(Hash(key, salt))
}

type Checker interface {
	Contains(hash uint64) bool
}

func Hash(key, salt []byte) uint64 {
	hash := sha256.New()
	// sha256.Write never returns errors
	_, _ = hash.Write(key)
	_, _ = hash.Write(salt)

	output := make([]byte, 0, sha256.Size)
	return binary.BigEndian.Uint64(hash.Sum(output))
}
