// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package random

import (
	"crypto/rand"
	"encoding/binary"
)

// NewMasterseed Compute a uniformly random series of 32 bytes.
//               Errors if the underlying generator does not have sufficient
//               entropy.
func NewMasterseed() ([32]byte, error) {
	bits := [32]byte{}
	_, err := rand.Read(bits[:])
	return bits, err
}

// NewNonce Compute a uniformly random uint64.
//          Errors if the underlying generator does not have sufficient
//          entropy.
func NewNonce() (uint64, error) {
	bits := [8]byte{}
	_, err := rand.Read(bits[:])
	return binary.BigEndian.Uint64(bits[:]), err
}
