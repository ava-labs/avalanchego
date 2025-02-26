// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewooddb

const PrefixDelimiter = '/'

// Prefix adds a prefix + delimiter
// prefix must not contain the delimiter
func Prefix(prefix []byte, key []byte) []byte {
	k := make([]byte, len(prefix)+len(key))

	copy(k, prefix)
	k[len(prefix)]= PrefixDelimiter
	copy(k[len(prefix)+1:], key)

	return k
}
