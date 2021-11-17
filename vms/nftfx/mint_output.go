// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type MintOutput struct {
	GroupID                  uint32 `serialize:"true" json:"groupID"`
	secp256k1fx.OutputOwners `serialize:"true"`
}

// MarshalJSON marshals Amt and the embedded OutputOwners struct
// into a JSON readable format
// If OutputOwners cannot be serialised then this will return error
func (out *MintOutput) MarshalJSON() ([]byte, error) {
	result, err := out.OutputOwners.Fields()
	if err != nil {
		return nil, err
	}

	result["groupID"] = out.GroupID
	return json.Marshal(result)
}
