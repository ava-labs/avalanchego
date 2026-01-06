// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"encoding/json"
	"errors"

	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	_ verify.State = (*TransferOutput)(nil)

	ErrNoValueOutput = errors.New("output has no value")
)

type TransferOutput struct {
	verify.IsState `json:"-"`

	Amt uint64 `serialize:"true" json:"amount"`

	OutputOwners `serialize:"true"`
}

// MarshalJSON marshals Amt and the embedded OutputOwners struct
// into a JSON readable format
// If OutputOwners cannot be serialized then this will return error
func (out *TransferOutput) MarshalJSON() ([]byte, error) {
	result, err := out.OutputOwners.Fields()
	if err != nil {
		return nil, err
	}

	result["amount"] = out.Amt
	return json.Marshal(result)
}

// Amount returns the quantity of the asset this output consumes
func (out *TransferOutput) Amount() uint64 {
	return out.Amt
}

func (out *TransferOutput) Verify() error {
	switch {
	case out == nil:
		return ErrNilOutput
	case out.Amt == 0:
		return ErrNoValueOutput
	default:
		return out.OutputOwners.Verify()
	}
}

func (out *TransferOutput) Owners() interface{} {
	return &out.OutputOwners
}
