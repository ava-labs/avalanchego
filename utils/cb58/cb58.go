// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cb58

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	"github.com/mr-tron/base58/base58"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

const checksumLen = 4

var (
	ErrBase58Decoding   = errors.New("base58 decoding error")
	ErrMissingChecksum  = errors.New("input string is smaller than the checksum size")
	ErrBadChecksum      = errors.New("invalid input checksum")
	ErrInputTooLong     = errors.New("input string is too long")
	errEncodingOverFlow = errors.New("encoding overflow")
)

// Encode [bytes] to a string using cb58 format.
// [bytes] may be nil, in which case it will be treated the same as an empty
// slice.
func Encode(bytes []byte) (string, error) {
	bytesLen := len(bytes)
	if bytesLen > math.MaxInt32-checksumLen {
		return "", errEncodingOverFlow
	}
	checked := make([]byte, bytesLen+checksumLen)
	copy(checked, bytes)
	copy(checked[len(bytes):], hashing.Checksum(bytes, checksumLen))
	return base58.Encode(checked), nil
}

// MaxEncodedLen returns an upper bound on the cb58 encoding length of a
// [payloadLen]-byte payload. The bound is exact for 20, 32, and 65-byte
// payloads and at most one character loose below ~230 bytes.
func MaxEncodedLen(payloadLen int) int {
	// base58 rewrites the input from base 256 (one byte per digit) to base
	// 58 (one character per digit), so n bytes need
	// ceil(n * log(256)/log(58)) = ceil(n * 1.36565...) characters. Rounding
	// the ratio up to 1.37 and adding 1 to the floor never underestimates.
	return (payloadLen+checksumLen)*137/100 + 1
}

// Decode [str] to bytes from cb58. [str] is rejected without decoding if it
// is longer than any encoding of a [maxLen]-byte payload.
func Decode(str string, maxLen int) ([]byte, error) {
	if maxInputLen := MaxEncodedLen(maxLen); len(str) > maxInputLen {
		return nil, fmt.Errorf("%w: got %d characters, max %d", ErrInputTooLong, len(str), maxInputLen)
	}
	decodedBytes, err := base58.Decode(str)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBase58Decoding, err)
	}
	if len(decodedBytes) < checksumLen {
		return nil, ErrMissingChecksum
	}
	// Verify the checksum
	rawBytes := decodedBytes[:len(decodedBytes)-checksumLen]
	checksum := decodedBytes[len(decodedBytes)-checksumLen:]
	if !bytes.Equal(checksum, hashing.Checksum(rawBytes, checksumLen)) {
		return nil, ErrBadChecksum
	}
	return rawBytes, nil
}
