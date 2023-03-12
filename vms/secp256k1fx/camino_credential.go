// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var errMSigNotUniqueOrSorted = errors.New("multisig aliases not sorted or unique")

type CredentialIntf interface {
	verify.Verifiable
	Signatures() [][crypto.SECP256K1RSigLen]byte
}

type MultisigCredential struct {
	Credential `serialize:"true"`
	// Specifies which MultisigAliases should be used during verification
	// Aliases which are not part of of this list are skipped (non-verified)
	MultiSigAliases []ids.ShortID `serialize:"true" json:"multisigAliases"`
}

func (cr *Credential) Signatures() [][crypto.SECP256K1RSigLen]byte {
	return cr.Sigs
}

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
	jsonFieldMap["multisigAliases"] = mcr.MultiSigAliases
	return json.Marshal(jsonFieldMap)
}

func (mcr *MultisigCredential) Verify() error {
	switch {
	case mcr == nil:
		return errNilCredential
	case !utils.IsSortedAndUniqueSortable(mcr.MultiSigAliases):
		return errMSigNotUniqueOrSorted
	default:
		return nil
	}
}

func (mcr *MultisigCredential) HasMultisig(id ids.ShortID) bool {
	for _, s := range mcr.MultiSigAliases {
		switch bytes.Compare(s[:], id[:]) {
		case 0:
			return true
		case 1:
			return false
		}
	}
	return false
}
