// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"encoding/json"
	"errors"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var errSigIdxsNotUniqueOrSorted = errors.New("signature indices not sorted or unique")

type CredentialIntf interface {
	verify.Verifiable
	Signatures() [][secp256k1.SignatureLen]byte
	SignatureIndices() []uint32
}

type MultisigCredential struct {
	Credential `serialize:"true"`
	// Overwrites sigIdx in inputs of a tx
	// Must be same length as sigIdx in input
	SigIdxs []uint32 `serialize:"true"`
}

/************ AVAX ***********/

func (cr *Credential) Signatures() [][secp256k1.SignatureLen]byte {
	return cr.Sigs
}

func (*Credential) SignatureIndices() []uint32 {
	return nil
}

/************ Camino ***********/

// MarshalJSON marshals [cr] to JSON
// The string representation of each signature is created using the hex formatter
func (mcr *MultisigCredential) MarshalJSON() ([]byte, error) {
	b, err := mcr.Credential.MarshalJSON()
	if err != nil {
		return nil, err
	}

	jsonFieldMap := make(map[string]interface{}, 0)
	if err = json.Unmarshal(b, &jsonFieldMap); err != nil {
		return nil, err
	}
	jsonFieldMap["sigIdxs"] = mcr.SigIdxs
	return json.Marshal(jsonFieldMap)
}

func (mcr *MultisigCredential) SignatureIndices() []uint32 {
	return mcr.SigIdxs
}

func (mcr *MultisigCredential) Verify() error {
	switch {
	case mcr == nil:
		return ErrNilCredential
	case !utils.IsSortedAndUniqueOrdered(mcr.SigIdxs):
		return errSigIdxsNotUniqueOrSorted
	default:
		return nil
	}
}
