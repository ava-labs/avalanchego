// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"errors"
	"strings"
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

/*
// EncodingManager is an interface to provide an Encoding interface
type EncodingManager interface {
	GetEncoder(encoding Encoding) (Encoder, error)
}

type manager struct {
	defaultEnc Encoding
}

// NewEncodingManager returns an EncodingManager with the provided default
func NewEncodingManager(defaultEnc Encoding) (EncodingManager, error) {
	if defaultEnc != Hex && defaultEnc != CB58 {
		return nil, fmt.Errorf("unrecognized default encoding: %s", defaultEnc)
	}
	return &manager{defaultEnc: defaultEnc}, nil
}

// GetEncoding returns a struct to be used for the given encoding
// TODO: Does this need to return an error?
func (m *manager) GetEncoder(encoding Encoding) (Encoder, error) {
	switch encoding {
	case Hex:
		return &hexEncoder{}, nil
	case CB58:
		return &cb58Encoder{}, nil
	default:
		return nil, errors.New("invalid encoder")
	}
}
*/
