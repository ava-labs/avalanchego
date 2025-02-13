// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/utils/formatting"
)

const nullStr = "null"

// JSONByteSlice represents [[]byte] that is json marshalled to hex
type JSONByteSlice []byte

func (b JSONByteSlice) MarshalJSON() ([]byte, error) {
	if b == nil {
		return []byte(nullStr), nil
	}

	hexData, err := formatting.Encode(formatting.HexNC, b)
	if err != nil {
		return nil, err
	}
	return json.Marshal(hexData)
}

func (b *JSONByteSlice) UnmarshalJSON(jsonBytes []byte) error {
	if string(jsonBytes) == nullStr {
		return nil
	}

	var hexData string
	if err := json.Unmarshal(jsonBytes, &hexData); err != nil {
		return err
	}
	v, err := formatting.Decode(formatting.HexNC, hexData)
	if err != nil {
		return err
	}
	*b = v
	return nil
}
