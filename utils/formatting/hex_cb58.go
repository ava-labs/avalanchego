// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"encoding/hex"
	"strings"

	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/mr-tron/base58/base58"
)

// HexCB58 formats bytes in Hex or CB58 encoding based on the value of Hex
type HexCB58 struct {
	Bytes []byte
	Hex   bool
}

// UnmarshalJSON ...
func (h *HexCB58) UnmarshalJSON(b []byte) error {
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
func (h HexCB58) MarshalJSON() ([]byte, error) { return []byte("\"" + h.String() + "\""), nil }

// FromString ...
func (h *HexCB58) FromString(str string) error {
	var rawBytes []byte
	var err error
	if strings.HasPrefix(str, "0x") {
		rawBytes, err = hexFromString(str)
	} else {
		rawBytes, err = cb58FromString(str)
	}

	if err == nil {
		h.Bytes = rawBytes
	}
	return err
}

// String ...
func (h HexCB58) String() string {
	checked := make([]byte, len(h.Bytes)+4)
	copy(checked, h.Bytes)
	copy(checked[len(h.Bytes):], hashing.Checksum(h.Bytes, 4))
	if h.Hex {
		return "0x" + hex.EncodeToString(checked)
	}

	return base58.Encode(checked)
}
