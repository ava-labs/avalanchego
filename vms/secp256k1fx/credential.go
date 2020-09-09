// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanche-go/utils/crypto"
	"github.com/ava-labs/avalanche-go/utils/formatting"
)

var (
	errNilCredential = errors.New("nil credential")
)

// Credential ...
type Credential struct {
	Sigs [][crypto.SECP256K1RSigLen]byte `serialize:"true" json:"signatures"`
}

// MarshalJSON marshals [cr] to JSON
func (cr *Credential) MarshalJSON() ([]byte, error) {
	buffer := bytes.NewBufferString("{\"signatures\":[")
	for i, sig := range cr.Sigs {
		buffer.WriteString(fmt.Sprintf("\"%s\"", formatting.CB58{Bytes: sig[:]}))
		if i != len(cr.Sigs)-1 {
			buffer.WriteString(",")
		}
	}
	buffer.WriteString("]}")
	return buffer.Bytes(), nil
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
