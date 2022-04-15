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

package types

import (
	"encoding/json"

	"github.com/chain4travel/caminogo/utils/formatting"
)

// JSONByteSlice represents [[]byte] that is json marshalled to hex
type JSONByteSlice []byte

func (b JSONByteSlice) MarshalJSON() ([]byte, error) {
	hexData, err := formatting.EncodeWithoutChecksum(formatting.Hex, b)
	if err != nil {
		return nil, err
	}
	return json.Marshal(hexData)
}
