// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/utils/formatting"
)

// JSONByteSlice represents [[]byte] that is json marshalled to hex
type JSONByteSlice []byte

func (b JSONByteSlice) MarshalJSON() ([]byte, error) {
	hexData, err := formatting.Encode(formatting.HexNC, b)
	if err != nil {
		return nil, err
	}
	return json.Marshal(hexData)
}

func (b *JSONByteSlice) UnmarshalJSON(data []byte) error {
	var hexData string

	if err := json.Unmarshal(data, &hexData); err != nil {
		return err
	}
	decoded, err := formatting.Decode(formatting.HexNC, hexData)
	if err != nil {
		return err
	}

	*b = decoded
	return nil
}
