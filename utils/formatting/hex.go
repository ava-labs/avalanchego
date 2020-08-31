// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"encoding/hex"
	"errors"
	"strings"
)

var (
	errMissingHexPrefix = errors.New("missing 0x prefix to hex encoding")
)

// HexWrapper formats bytes in hexadecimal encoding
type HexWrapper struct{ Bytes []byte }

// UnmarshalJSON ...
func (h *HexWrapper) UnmarshalJSON(b []byte) error {
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
func (h HexWrapper) MarshalJSON() ([]byte, error) { return []byte("\"" + h.String() + "\""), nil }

// FromString ...
func (h *HexWrapper) FromString(str string) error {
	if !strings.HasPrefix(str, "0x") {
		return errMissingHexPrefix
	}
	rawBytes, err := hex.DecodeString(str[2:])
	if err != nil {
		return err
	}
	h.Bytes = rawBytes
	return nil
}

func (h HexWrapper) String() string {
	return "0x" + hex.EncodeToString(h.Bytes)
}
