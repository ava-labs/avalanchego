// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"encoding/json"

	"github.com/ava-labs/libevm/common"

	_ "embed"
)

var (
	//go:embed fuji_ext_data_hashes.json
	rawFujiExtDataHashes []byte
	fujiExtDataHashes    map[common.Hash]common.Hash

	//go:embed mainnet_ext_data_hashes.json
	rawMainnetExtDataHashes []byte
	mainnetExtDataHashes    map[common.Hash]common.Hash
)

func init() {
	if err := json.Unmarshal(rawFujiExtDataHashes, &fujiExtDataHashes); err != nil {
		panic(err)
	}
	rawFujiExtDataHashes = nil
	if err := json.Unmarshal(rawMainnetExtDataHashes, &mainnetExtDataHashes); err != nil {
		panic(err)
	}
	rawMainnetExtDataHashes = nil
}
