package evm

import (
	_ "embed"
	"encoding/json"

	"github.com/ethereum/go-ethereum/common"
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
