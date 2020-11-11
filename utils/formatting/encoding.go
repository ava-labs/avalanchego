// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"fmt"
)

const (
	// HexEncoding specifies a hex plus 4 byte checksum encoding format
	HexEncoding = "hex"
	// CB58Encoding specifies the CB58 encoding format
	CB58Encoding = "cb58"
)

// Encoding returns a struct used to format bytes for a specific encoding
type Encoding interface {
	ConvertBytes([]byte) (string, error)
	ConvertString(str string) ([]byte, error)
	Encoding() string
}

// EncodingManager is an interface to provide an Encoding interface
type EncodingManager interface {
	GetEncoding(encoding string) (Encoding, error)
}

type manager struct {
	defaultEnc string
}

// NewEncodingManager returns an EncodingManager with the provided default
func NewEncodingManager(defaultEnc string) (EncodingManager, error) {
	if defaultEnc != HexEncoding && defaultEnc != CB58Encoding {
		return nil, fmt.Errorf("unrecognized default encoding: %s", defaultEnc)
	}
	return &manager{defaultEnc: defaultEnc}, nil
}

// GetEncoding returns a struct to be used for the given encoding
func (m *manager) GetEncoding(encoding string) (Encoding, error) {
	if encoding == "" {
		encoding = m.defaultEnc
	}
	switch encoding {
	case HexEncoding:
		return &Hex{}, nil
	case CB58Encoding:
		return &CB58{}, nil
	default:
		return nil, fmt.Errorf("unrecognized encoding format: %s", encoding)
	}
}
