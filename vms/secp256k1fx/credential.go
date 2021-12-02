// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

var errNilCredential = errors.New("nil credential")

const (
	defaultEncoding = formatting.Hex
)

type Credential struct {
	Sigs [][crypto.SECP256K1RSigLen]byte `serialize:"true" json:"signatures"`
}

// MarshalJSON marshals [cr] to JSON
// The string representation of each signature is created using the hex formatter
func (cr *Credential) MarshalJSON() ([]byte, error) {
	signatures := make([]string, len(cr.Sigs))
	for i, sig := range cr.Sigs {
		sigStr, err := formatting.EncodeWithoutChecksum(defaultEncoding, sig[:])
		if err != nil {
			return nil, fmt.Errorf("couldn't convert signature to string: %w", err)
		}
		signatures[i] = sigStr
	}
	jsonFieldMap := map[string]interface{}{
		"signatures": signatures,
	}
	return json.Marshal(jsonFieldMap)
}

func (cr *Credential) Verify() error {
	switch {
	case cr == nil:
		return errNilCredential
	default:
		return nil
	}
}
