// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	json2 "encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

var errNilCredential = errors.New("nil credential")

const (
	defaultEncoding = formatting.Hex
)

type FxCredential interface {
	InitFx(fxID ids.ID)
}

// Credential ...
type Credential struct {
	FxID ids.ID                          `serialize:"true" json:"fxID"`
	Sigs [][crypto.SECP256K1RSigLen]byte `serialize:"true" json:"signatures"`
}

func (cr *Credential) InitFx(fxID ids.ID) {
	cr.FxID = fxID
}

// MarshalJSON marshals [cr] to JSON
// The string representation of each signature is created using the hex formatter
func (cr *Credential) MarshalJSON() ([]byte, error) {
	jsonFieldMap := make(map[string]interface{}, 2)
	jsonFieldMap["fxID"] = cr.FxID
	signatures := make([]string, len(cr.Sigs))

	for i, sig := range cr.Sigs {
		sigStr, err := formatting.Encode(defaultEncoding, sig[:])
		if err != nil {
			return nil, fmt.Errorf("couldn't convert signature to string: %w", err)
		}
		signatures[i] = sigStr
	}

	jsonFieldMap["signatures"] = signatures
	b, err := json2.Marshal(jsonFieldMap)
	return b, err
}

// Verify ...
func (cr *Credential) Verify() error {
	switch {
	case cr == nil:
		return errNilCredential
	default:
		return nil
	}
}
