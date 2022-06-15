// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/config"
)

func getCreateBlockchainTxFee(cfg config.Config, t time.Time) uint64 {
	if t.Before(cfg.ApricotPhase3Time) {
		return cfg.CreateAssetTxFee
	}
	return cfg.CreateBlockchainTxFee
}

func getCreateSubnetTxFee(cfg config.Config, t time.Time) uint64 {
	if t.Before(cfg.ApricotPhase3Time) {
		return cfg.CreateAssetTxFee
	}
	return cfg.CreateSubnetTxFee
}
