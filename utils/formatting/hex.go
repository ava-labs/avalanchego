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

// hexEncoder implements the Encoder interface
// Provides a hex format with 4 byte checksum
type hexEncoder struct{}

// ConvertString ...
func (h hexEncoder) ConvertString(str string) ([]byte, error) {
	if len(str) == 0 {
		return []byte{}, nil
	}
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

// ConvertBytes returns the string representation of [b]
// Always returns a nil error
func (h hexEncoder) ConvertBytes(b []byte) (string, error) {
	checked := make([]byte, len(b)+4)
	copy(checked, b)
	copy(checked[len(b):], hashing.Checksum(b, 4))
	return fmt.Sprintf("0x%x", checked), nil
}

// Encoding ...
func (h *hexEncoder) Encoding() Encoding { return Hex }
