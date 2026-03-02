// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

var ErrNilCredential = errors.New("nil credential")

type Credential struct {
	Sigs [][secp256k1.SignatureLen]byte `serialize:"true" json:"signatures"`
}

// MarshalJSON marshals [cr] to JSON
// The string representation of each signature is created using the hex formatter
func (cr *Credential) MarshalJSON() ([]byte, error) {
	signatures := make([]string, len(cr.Sigs))
	for i, sig := range cr.Sigs {
		sigStr, err := formatting.Encode(formatting.HexNC, sig[:])
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
	if cr == nil {
		return ErrNilCredential
	}

	return nil
}
