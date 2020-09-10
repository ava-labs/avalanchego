// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"bytes"
	"errors"

	"github.com/mr-tron/base58/base58"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	errMissingQuotes   = errors.New("missing quotes")
	errMissingChecksum = errors.New("input string is smaller than the checksum size")
	errBadChecksum     = errors.New("invalid input checksum")
)

// CB58 formats bytes in checksummed base-58 encoding
type CB58 struct{ Bytes []byte }

// UnmarshalJSON ...
func (cb58 *CB58) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == "null" {
		return nil
	}

	if len(str) < 2 {
		return errMissingQuotes
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}
	return cb58.FromString(str[1:lastIndex])
}

// MarshalJSON ...
func (cb58 CB58) MarshalJSON() ([]byte, error) { return []byte("\"" + cb58.String() + "\""), nil }

// FromString ...
func (cb58 *CB58) FromString(str string) error {
	b, err := base58.Decode(str)
	if err != nil {
		return err
	}
	if len(b) < 4 {
		return errMissingChecksum
	}

	rawBytes := b[:len(b)-4]
	checksum := b[len(b)-4:]

	if !bytes.Equal(checksum, hashing.Checksum(rawBytes, 4)) {
		return errBadChecksum
	}

	cb58.Bytes = rawBytes
	return nil
}

func (cb58 CB58) String() string {
	checked := make([]byte, len(cb58.Bytes)+4)
	copy(checked, cb58.Bytes)
	copy(checked[len(cb58.Bytes):], hashing.Checksum(cb58.Bytes, 4))
	return base58.Encode(checked)
}
