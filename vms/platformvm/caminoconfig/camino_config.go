// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package caminoconfig

import "github.com/ava-labs/avalanchego/vms/platformvm/dac"

type Config struct {
	DACProposalBondAmount uint64
	// Fractions of transaction fee distribution
	FeeDistribution [dac.FeeDistributionFractionsCount]uint64 `json:"feeDistribution"`
}
