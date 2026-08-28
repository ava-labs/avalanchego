// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/hex"
	"encoding/json"
	"github.com/ava-labs/avalanchego/ids"
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/gas"
)

// DefaultWarpHelperAddresses are the Nick-deployed PChain.sol addresses per
// network, used when the chain config does not list any. Regenerate with
// `go run ./tests/warpauth/nick -network <name>` whenever PChain.sol changes;
// tests/warpauth pins these to the contract bytes.
var DefaultWarpHelperAddresses = map[uint32][]ids.ShortID{
	constants.MainnetID: {mustShortID("0x2c9Bf34aA13CC68309AE60D91A902413BabB6423")},
	constants.FujiID:    {mustShortID("0x81B698Bf5D21E868Fa8cc1E30555E604A067B045")},
}

func mustShortID(hexAddr string) ids.ShortID {
	b, err := hex.DecodeString(hexAddr[2:])
	if err != nil {
		panic(err)
	}
	return ids.ShortID(b)
}

var Default = Config{
	Network:                       DefaultNetwork,
	BlockCacheSize:                64 * units.MiB,
	TxCacheSize:                   128 * units.MiB,
	TransformedSubnetTxCacheSize:  4 * units.MiB,
	RewardUTXOsCacheSize:          2048,
	ChainCacheSize:                2048,
	ChainDBCacheSize:              2048,
	BlockIDCacheSize:              8192,
	FxOwnerCacheSize:              4 * units.MiB,
	SubnetToL1ConversionCacheSize: 4 * units.MiB,
	L1WeightsCacheSize:            16 * units.KiB,
	L1InactiveValidatorsCacheSize: 256 * units.KiB,
	L1SubnetIDNodeIDCacheSize:     16 * units.KiB,
	ChecksumsEnabled:              false,
	MempoolPruneFrequency:         30 * time.Minute,
	MempoolGasCapacity:            1_000_000,
}

// Config contains all of the user-configurable parameters of the PlatformVM.
type Config struct {
	Network Network `json:"network"`
	// WarpHelperAddresses are the C-chain contracts trusted to send warp
	// credentials on behalf of the owner named in the payload.
	WarpHelperAddresses           []ids.ShortID `json:"warp-helper-addresses"`
	BlockCacheSize                int           `json:"block-cache-size"`
	TxCacheSize                   int           `json:"tx-cache-size"`
	TransformedSubnetTxCacheSize  int           `json:"transformed-subnet-tx-cache-size"`
	RewardUTXOsCacheSize          int           `json:"reward-utxos-cache-size"`
	ChainCacheSize                int           `json:"chain-cache-size"`
	ChainDBCacheSize              int           `json:"chain-db-cache-size"`
	BlockIDCacheSize              int           `json:"block-id-cache-size"`
	FxOwnerCacheSize              int           `json:"fx-owner-cache-size"`
	SubnetToL1ConversionCacheSize int           `json:"subnet-to-l1-conversion-cache-size"`
	L1WeightsCacheSize            int           `json:"l1-weights-cache-size"`
	L1InactiveValidatorsCacheSize int           `json:"l1-inactive-validators-cache-size"`
	L1SubnetIDNodeIDCacheSize     int           `json:"l1-subnet-id-node-id-cache-size"`
	ChecksumsEnabled              bool          `json:"checksums-enabled"`
	MempoolPruneFrequency         time.Duration `json:"mempool-prune-frequency"`
	MempoolGasCapacity            gas.Gas       `json:"mempool-gas-capacity"`
}

// GetConfig returns a Config from the provided json encoded bytes. If a
// configuration is not provided in the bytes, the default value is set. If
// empty bytes are provided, the default config is returned.
func GetConfig(b []byte) (*Config, error) {
	ec := Default

	// An empty slice is invalid json, so handle that as a special case.
	if len(b) == 0 {
		return &ec, nil
	}

	return &ec, json.Unmarshal(b, &ec)
}
