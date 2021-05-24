// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashing

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/ripemd160"
)

var errBadLength = errors.New("input has insufficient length")

// HashLen ...
const HashLen = sha256.Size

// AddrLen ...
const AddrLen = ripemd160.Size

// Hash256 A 256 bit long hash value.
type Hash256 = [HashLen]byte

// Hash160 A 160 bit long hash value.
type Hash160 = [ripemd160.Size]byte

// ComputeHash256Array Compute a cryptographically strong 256 bit hash of the
//                     input byte slice.
func ComputeHash256Array(buf []byte) Hash256 {
	return sha256.Sum256(buf)
}

// ComputeHash256 Compute a cryptographically strong 256 bit hash of the input
//                byte slice.
func ComputeHash256(buf []byte) []byte {
	arr := ComputeHash256Array(buf)
	return arr[:]
}

// ByteArraysToHash256Array takes in byte arrays and outputs a fixed 32 length
//					byte array for the hash
func ByteArraysToHash256Array(byteArray ...[]byte) [32]byte {
	buffer := new(bytes.Buffer)
	for _, b := range byteArray {
		err := binary.Write(buffer, binary.LittleEndian, b)
		if err != nil {
			panic(err)
		}
	}
	return ComputeHash256Array(buffer.Bytes())
}

// ComputeHash256Ranges Compute a cryptographically strong 256 bit hash of the input
//                      byte slice in the ranges specified.
// Example: ComputeHash256Ranges({1, 2, 4, 8, 16}, {{1, 2},
//                                                  {3, 5}})
//          is equivalent to ComputeHash256({2, 8, 16}).
func ComputeHash256Ranges(buf []byte, ranges [][2]int) []byte {
	hashBuilder := sha256.New()
	for _, r := range ranges {
		_, err := hashBuilder.Write(buf[r[0]:r[1]])
		if err != nil {
			panic(err)
		}
	}
	return hashBuilder.Sum(nil)
}

// ComputeHash160Array Compute a cryptographically strong 160 bit hash of the
//                     input byte slice.
func ComputeHash160Array(buf []byte) Hash160 {
	h, err := ToHash160(ComputeHash160(buf))
	if err != nil {
		panic(err)
	}
	return h
}

// ComputeHash160 Compute a cryptographically strong 160 bit hash of the input
//                byte slice.
func ComputeHash160(buf []byte) []byte {
	ripe := ripemd160.New()
	_, err := io.Writer(ripe).Write(buf)
	if err != nil {
		panic(err)
	}
	return ripe.Sum(nil)
}

// Checksum Create checksum of [length] bytes from the 256 bit hash of the byte slice.
//          Returns the lower [length] bytes of the hash
//          Errors if length > 32.
func Checksum(bytes []byte, length int) []byte {
	hash := ComputeHash256Array(bytes)
	return hash[len(hash)-length:]
}

// ToHash256 ...
func ToHash256(bytes []byte) (Hash256, error) {
	hash := Hash256{}
	if len(bytes) != HashLen {
		return hash, errBadLength
	}
	copy(hash[:], bytes)
	return hash, nil
}

// ToHash160 ...
func ToHash160(bytes []byte) (Hash160, error) {
	hash := Hash160{}
	if len(bytes) != ripemd160.Size {
		return hash, errBadLength
	}
	copy(hash[:], bytes)
	return hash, nil
}

// PubkeyBytesToAddress ...
func PubkeyBytesToAddress(key []byte) []byte {
	return ComputeHash160(ComputeHash256(key))
}
