// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

// Tx that this Fx is supporting
type Tx interface {
	UnsignedBytes() []byte
}
