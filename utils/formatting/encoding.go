// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
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
	// maximum length byte slice can be encoded as a string
	// using the CB58 encoding. Must be longer than the length
	// of an ID and longer than the length of a SECP256k1 private key
	// TODO: Reduce to a reasonable amount (e.g. 16 KiB) after we
	// give users a chance to export very large keystore users to hex
	maxCB58EncodeSize = math.MaxInt32
	hexPrefix         = "0x"
	checksumLen       = 4
)

var (
	// Maximum length CB58 encoded string that can be decoded to bytes
	// This is different than [maxCB58EncodeSize] because each byte can express up
	// to 256 but each base 58 digit can express up to 58
	// The 10 is because there seems to be a floating point issue where the calculated
	// max decode size (using this formula) is slightly smaller than the actual
	maxCB58DecodeSize              = int(float64(maxCB58EncodeSize)*math.Log2(256)/math.Log2(58)) + 10
	errEncodingOverFlow            = errors.New("encoding overflow")
	errInvalidEncoding             = errors.New("invalid encoding")
	errUnsupportedEncodingInMethod = errors.New("unsupported encoding in method")
	errMissingChecksum             = errors.New("input string is smaller than the checksum size")
	errBadChecksum                 = errors.New("invalid input checksum")
	errMissingHexPrefix            = errors.New("missing 0x prefix to hex encoding")
)

// Encoding defines how bytes are converted to a string and vice versa
type Encoding uint8

const (
	// CB58 specifies the CB58 encoding format
	CB58 Encoding = iota
	// Hex specifies a hex plus 4 byte checksum encoding format
	Hex
	// JSON specifies the JSON encoding format
	JSON
)

func (enc Encoding) String() string {
	switch enc {
	case Hex:
		return "hex"
	case CB58:
		return "cb58"
	case JSON:
		return "json"
	default:
		return errInvalidEncoding.Error()
	}
}

func (enc Encoding) valid() bool {
	switch enc {
	case Hex, CB58, JSON:
		return true
	}
	return false
}

func (enc Encoding) MarshalJSON() ([]byte, error) {
	if !enc.valid() {
		return nil, errInvalidEncoding
	}
	return []byte("\"" + enc.String() + "\""), nil
}

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
	case "\"json\"":
		*enc = JSON
	default:
		return errInvalidEncoding
	}
	return nil
}

// EncodeWithChecksum [bytes] to a string using the given encoding format
// [bytes] may be nil, in which case it will be treated the same
// as an empty slice.
// This function includes a checksum in the encoded string.
func EncodeWithChecksum(encoding Encoding, bytes []byte) (string, error) {
	if err := validateEncoding(encoding, bytes); err != nil {
		return "", err
	}

	bytesLen := len(bytes)
	if bytesLen > math.MaxInt32-checksumLen {
		return "", errEncodingOverFlow
	}
	checked := make([]byte, bytesLen+checksumLen)
	copy(checked, bytes)
	copy(checked[len(bytes):], hashing.Checksum(bytes, checksumLen))
	return encode(encoding, checked)
}

// EncodeWithoutChecksum [bytes] to a string using the given encoding format
// [bytes] may be nil, in which case it will be treated the same
// as an empty slice.
// Unlike EncodeWithChecksum, this function does not include a checksum in the
// encoded string.
func EncodeWithoutChecksum(encoding Encoding, bytes []byte) (string, error) {
	if err := validateEncoding(encoding, bytes); err != nil {
		return "", err
	}
	return encode(encoding, bytes)
}

// encode encodes given [bytes] to [encoding] format
// validateEncoding([encoding],[bytes]) should be called before this
func encode(encoding Encoding, bytes []byte) (string, error) {
	switch encoding {
	case Hex:
		return fmt.Sprintf("0x%x", bytes), nil
	case CB58:
		return base58.Encode(bytes), nil
	case JSON:
		// JSON Marshal does not support []byte input and we rely on the
		// router's json marshalling to marshal our interface{} into JSON
		// in response. Therefore it is not supported in this call.
		return "", errUnsupportedEncodingInMethod
	default:
		return "", errInvalidEncoding
	}
}

// validateEncoding validates given [encoding] to [bytes]
// Returns error if encoding is invalid or not applicable
func validateEncoding(encoding Encoding, bytes []byte) error {
	switch {
	case !encoding.valid():
		return errInvalidEncoding
	case encoding == CB58 && len(bytes) > maxCB58EncodeSize:
		return fmt.Errorf("byte slice length (%d) > maximum for cb58 (%d)", len(bytes), maxCB58EncodeSize)
	}
	return nil
}

// Decode [str] to bytes using the given encoding
// If [str] is the empty string, returns a nil byte slice and nil error
func Decode(encoding Encoding, str string) ([]byte, error) {
	switch {
	case !encoding.valid():
		return nil, errInvalidEncoding
	case len(str) == 0:
		return nil, nil
	case encoding == CB58 && len(str) > maxCB58DecodeSize:
		return nil, fmt.Errorf("string length (%d) > maximum for cb58 (%d)", len(str), maxCB58DecodeSize)
	}

	var (
		decodedBytes []byte
		err          error
	)
	switch encoding {
	case Hex:
		if !strings.HasPrefix(str, hexPrefix) {
			return nil, errMissingHexPrefix
		}
		decodedBytes, err = hex.DecodeString(str[2:])
	case CB58:
		decodedBytes, err = base58.Decode(str)
	case JSON:
		// JSON unmarshalling requires interface and has no return values
		// contrary to this method, therefore it is not supported in this call
		return nil, errUnsupportedEncodingInMethod
	default:
		return nil, errInvalidEncoding
	}

	if err != nil {
		return nil, err
	}
	if len(decodedBytes) < checksumLen {
		return nil, errMissingChecksum
	}
	// Verify the checksum
	rawBytes := decodedBytes[:len(decodedBytes)-checksumLen]
	if len(rawBytes) > maxCB58EncodeSize {
		return nil, fmt.Errorf("byte slice length (%d) > maximum for cb58 (%d)", len(decodedBytes), maxCB58EncodeSize)
	}

	checksum := decodedBytes[len(decodedBytes)-checksumLen:]
	if !bytes.Equal(checksum, hashing.Checksum(rawBytes, checksumLen)) {
		return nil, errBadChecksum
	}
	return rawBytes, nil
}
