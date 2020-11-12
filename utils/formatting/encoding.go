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
	"github.com/mr-tron/base58/base58"
)

var (
	errInvalidEncoding = errors.New("invalid encoding")
)

type Encoding uint8

const (
	// Hex specifies a hex plus 4 byte checksum encoding format
	Hex Encoding = iota
	// CB58 specifies the CB58 encoding format
	CB58
)

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
	str := strings.ToLower(string(b))

	switch str {
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
func Encode(encoding Encoding, bytes []byte) (string, error) {
	switch encoding {
	case Hex:
		checked := make([]byte, len(bytes)+4)
		copy(checked, bytes)
		copy(checked[len(bytes):], hashing.Checksum(bytes, 4))
		return fmt.Sprintf("0x%x", checked), nil
	case CB58:
		if len(bytes) > maxCB58Size {
			return "", fmt.Errorf("byte slice length (%d) > maximum for cb58 (%d)", len(bytes), maxCB58Size)
		}
		checked := make([]byte, len(bytes)+4)
		copy(checked, bytes)
		copy(checked[len(bytes):], hashing.Checksum(bytes, 4))
		return base58.Encode(checked), nil
	default:
		return "", errInvalidEncoding
	}
}

// Decode [str] to bytes using the given encoding
// If [str] is the empty string, returns a nil byte slice
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
	} else if len(decodedBytes) < 4 {
		return nil, errMissingChecksum
	}
	rawBytes := decodedBytes[:len(decodedBytes)-4]
	checksum := decodedBytes[len(decodedBytes)-4:]
	if !bytes.Equal(checksum, hashing.Checksum(rawBytes, 4)) {
		return nil, errBadChecksum
	}
	return decodedBytes, nil
}

// Encoding returns a struct used to format bytes for a specific encoding
type Encoder interface {
	ConvertBytes([]byte) (string, error)
	ConvertString(str string) ([]byte, error)
	Encoding() Encoding
}

// NewEncoder returns a new Encoder that uses the specified encoding
func NewEncoder(encoding Encoding) Encoder {
	switch encoding {
	case CB58:
		return &cb58Encoder{}
	default:
		return &hexEncoder{}
	}
}
