// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

var (
	errMissingHexPrefix = errors.New("missing 0x prefix to hex encoding")
)

// Hex formats bytes in hexadecimal encoding
type Hex struct{ Bytes []byte }

// UnmarshalJSON ...
func (h *Hex) UnmarshalJSON(b []byte) error {
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
	return h.FromString(str[1:lastIndex])
}

// MarshalJSON ...
func (h Hex) MarshalJSON() ([]byte, error) { return []byte("\"" + h.String() + "\""), nil }

// FromString ...
func (h *Hex) FromString(str string) error {
	rawBytes, err := hexFromString(str)
	if err == nil {
		h.Bytes = rawBytes
	}
	return err
}

// String ...
func (h Hex) String() string {
	checked := make([]byte, len(h.Bytes)+4)
	copy(checked, h.Bytes)
	copy(checked[len(h.Bytes):], hashing.Checksum(h.Bytes, 4))
	return fmt.Sprintf("0x%x", checked)
}

func hexFromString(str string) ([]byte, error) {
	if !strings.HasPrefix(str, "0x") {
		return nil, errMissingHexPrefix
	}
	b, err := hex.DecodeString(str[2:])
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
