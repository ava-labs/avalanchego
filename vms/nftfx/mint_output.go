// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ verify.State = (*MintOutput)(nil)

type MintOutput struct {
	verify.IsState `json:"-"`

	GroupID                  uint32 `serialize:"true" json:"groupID"`
	secp256k1fx.OutputOwners `serialize:"true"`
}

// MarshalJSON marshals Amt and the embedded OutputOwners struct
// into a JSON readable format
// If OutputOwners cannot be serialized then this will return error
func (out *MintOutput) MarshalJSON() ([]byte, error) {
	result, err := out.OutputOwners.Fields()
	if err != nil {
		return nil, err
	}

	result["groupID"] = out.GroupID
	return json.Marshal(result)
}
