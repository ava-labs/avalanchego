// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashing

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/ripemd160"
)

const (
	HashLen = sha256.Size
	AddrLen = ripemd160.Size
)

var ErrInvalidHashLen = errors.New("invalid hash length")

// Hash256 A 256 bit long hash value.
type Hash256 = [HashLen]byte

// Hash160 A 160 bit long hash value.
type Hash160 = [ripemd160.Size]byte

// ComputeHash256Array computes a cryptographically strong 256 bit hash of the
// input byte slice.
func ComputeHash256Array(buf []byte) Hash256 {
	return sha256.Sum256(buf)
}

// ComputeHash256 computes a cryptographically strong 256 bit hash of the input
// byte slice.
func ComputeHash256(buf []byte) []byte {
	arr := ComputeHash256Array(buf)
	return arr[:]
}

// ComputeHash160Array computes a cryptographically strong 160 bit hash of the
// input byte slice.
func ComputeHash160Array(buf []byte) Hash160 {
	h, err := ToHash160(ComputeHash160(buf))
	if err != nil {
		panic(err)
	}
	return h
}

// ComputeHash160 computes a cryptographically strong 160 bit hash of the input
// byte slice.
func ComputeHash160(buf []byte) []byte {
	ripe := ripemd160.New()
	_, err := io.Writer(ripe).Write(buf)
	if err != nil {
		panic(err)
	}
	return ripe.Sum(nil)
}

// Checksum creates a checksum of [length] bytes from the 256 bit hash of the
// byte slice.
//
// Returns: the lower [length] bytes of the hash
// Panics if length > 32.
func Checksum(bytes []byte, length int) []byte {
	hash := ComputeHash256Array(bytes)
	return hash[len(hash)-length:]
}

func ToHash256(bytes []byte) (Hash256, error) {
	hash := Hash256{}
	if bytesLen := len(bytes); bytesLen != HashLen {
		return hash, fmt.Errorf("%w: expected 32 bytes but got %d", ErrInvalidHashLen, bytesLen)
	}
	copy(hash[:], bytes)
	return hash, nil
}

func ToHash160(bytes []byte) (Hash160, error) {
	hash := Hash160{}
	if bytesLen := len(bytes); bytesLen != ripemd160.Size {
		return hash, fmt.Errorf("%w: expected 20 bytes but got %d", ErrInvalidHashLen, bytesLen)
	}
	copy(hash[:], bytes)
	return hash, nil
}

func PubkeyBytesToAddress(key []byte) []byte {
	return ComputeHash160(ComputeHash256(key))
}
