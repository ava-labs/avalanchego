// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/mr-tron/base58/base58"
)

const (
	// maximum length byte slice can be marshalled to a string
	// using the CB58 encoding. Must be longer than the length
	// of an ID and longer than the length of a SECP256k1 private key
	// TODO: Reduce to a reasonable amount (e.g. 16 * 1024) after we
	// give users a chance to export very large keystore users to hex
	maxCB58Size = math.MaxInt32

	checksumLen = 4
)

var (
	errInvalidEncoding  = errors.New("invalid encoding")
	errMissingChecksum  = errors.New("input string is smaller than the checksum size")
	errBadChecksum      = errors.New("invalid input checksum")
	errMissingHexPrefix = errors.New("missing 0x prefix to hex encoding")
)

// Encoding defines how bytes are converted to a string and vice versa
type Encoding uint8

const (
	// CB58 specifies the CB58 encoding format
	CB58 Encoding = iota
	// Hex specifies a hex plus 4 byte checksum encoding format
	Hex
)

// String ...
func (enc Encoding) String() string {
	switch enc {
	case Hex:
		return "hex"
	case CB58:
		return "cb58"
	default:
		return errInvalidEncoding.Error()
	}
}

func (enc Encoding) valid() bool {
	switch enc {
	case Hex, CB58:
		return true
	}
	return false
}

// MarshalJSON ...
func (enc Encoding) MarshalJSON() ([]byte, error) {
	if !enc.valid() {
		return nil, errInvalidEncoding
	}
	return []byte("\"" + enc.String() + "\""), nil
}

// UnmarshalJSON ...
func (enc *Encoding) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == "null" {
		return nil
	}
	switch strings.ToLower(str) {
	case "\"hex\"":
		*enc = Hex
	case "\"cb58\"":
		*enc = CB58
	default:
		return errInvalidEncoding
	}
	return nil
}

// Encode [bytes] to a string using the given encoding format
// [bytes] may be nil, in which case it will be treated the same
// as an empty slice
func Encode(encoding Encoding, bytes []byte) (string, error) {
	if !encoding.valid() {
		return "", errInvalidEncoding
	} else if encoding == CB58 && len(bytes) > maxCB58Size {
		return "", fmt.Errorf("byte slice length (%d) > maximum for cb58 (%d)", len(bytes), maxCB58Size)
	}

	checked := make([]byte, len(bytes)+checksumLen)
	copy(checked, bytes)
	copy(checked[len(bytes):], hashing.Checksum(bytes, checksumLen))
	switch encoding {
	case Hex:
		return fmt.Sprintf("0x%x", checked), nil
	case CB58:
		return base58.Encode(checked), nil
	default:
		return "", errInvalidEncoding
	}
}

// Decode [str] to bytes using the given encoding
// If [str] is the empty string, returns a nil byte slice and nil error
func Decode(encoding Encoding, str string) ([]byte, error) {
	if !encoding.valid() {
		return nil, errInvalidEncoding
	} else if len(str) == 0 {
		return nil, nil
	}

	var (
		decodedBytes []byte
		err          error
	)
	switch encoding {
	case Hex:
		if !strings.HasPrefix(str, "0x") {
			return nil, errMissingHexPrefix
		}
		decodedBytes, err = hex.DecodeString(str[2:])
	case CB58:
		decodedBytes, err = base58.Decode(str)
	}
	if err != nil {
		return nil, err
	} else if len(decodedBytes) < checksumLen {
		return nil, errMissingChecksum
	}
	// Verify the checksum
	rawBytes := decodedBytes[:len(decodedBytes)-checksumLen]
	checksum := decodedBytes[len(decodedBytes)-checksumLen:]
	if !bytes.Equal(checksum, hashing.Checksum(rawBytes, checksumLen)) {
		return nil, errBadChecksum
	}
	return rawBytes, nil
}
