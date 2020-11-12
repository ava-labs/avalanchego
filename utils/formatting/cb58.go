// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/mr-tron/base58/base58"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

const (
	// maximum length byte slice can be marshalled to a string
	// Must be longer than the length of an ID and longer than
	// the length of a SECP256k1 private key
	maxCB58Size = 16 * 1024 // 16 KB
)

var (
	errMissingChecksum = errors.New("input string is smaller than the checksum size")
	errBadChecksum     = errors.New("invalid input checksum")
)

// CB58 formats bytes in checksummed base-58 encoding
type CB58 struct{}

// ConvertBytes ...
func (cb58 CB58) ConvertBytes(b []byte) (string, error) {
	if len(b) > maxCB58Size {
		return "", fmt.Errorf("byte slice length (%d) > maximum for cb58 (%d)", len(b), maxCB58Size)
	}
	checked := make([]byte, len(b)+4)
	copy(checked, b)
	copy(checked[len(b):], hashing.Checksum(b, 4))
	return base58.Encode(checked), nil
}

// ConvertString ...
func (cb58 CB58) ConvertString(str string) ([]byte, error) {
	if len(str) == 0 {
		return []byte{}, nil
	}
	b, err := base58.Decode(str)
	if err != nil {
		return nil, err
	}
	if len(b) < 4 {
		return nil, errMissingChecksum
	}

	rawBytes := b[:len(b)-4]
	checksum := b[len(b)-4:]

	if !bytes.Equal(checksum, hashing.Checksum(rawBytes, 4)) {
		return nil, errBadChecksum
	}

	return rawBytes, nil
}

// Encoding ...
func (cb58 *CB58) Encoding() string { return CB58Encoding }
