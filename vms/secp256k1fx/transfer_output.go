// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"encoding/json"
	"errors"

	"github.com/chain4travel/caminogo/vms/components/verify"
)

var (
	errNoValueOutput              = errors.New("output has no value")
	_                verify.State = &OutputOwners{}
)

type TransferOutput struct {
	Amt uint64 `serialize:"true" json:"amount"`

	OutputOwners `serialize:"true"`
}

// MarshalJSON marshals Amt and the embedded OutputOwners struct
// into a JSON readable format
// If OutputOwners cannot be serialised then this will return error
func (out *TransferOutput) MarshalJSON() ([]byte, error) {
	result, err := out.OutputOwners.Fields()
	if err != nil {
		return nil, err
	}

	result["amount"] = out.Amt
	return json.Marshal(result)
}

// Amount returns the quantity of the asset this output consumes
func (out *TransferOutput) Amount() uint64 { return out.Amt }

func (out *TransferOutput) Verify() error {
	switch {
	case out == nil:
		return errNilOutput
	case out.Amt == 0:
		return errNoValueOutput
	default:
		return out.OutputOwners.Verify()
	}
}

func (out *TransferOutput) VerifyState() error { return out.Verify() }

func (out *TransferOutput) Owners() interface{} { return &out.OutputOwners }
