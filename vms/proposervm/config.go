// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"crypto"
	"time"

	"github.com/ava-labs/avalanchego/staking"
)

type Config struct {
	// Time at which proposerVM activates its congestion control mechanism
	ActivationTime time.Time

	// Durango fork activation time
	DurangoTime time.Time

	// Minimal P-chain height referenced upon block building
	MinimumPChainHeight uint64

	// Configurable minimal delay among blocks issued consecutively
	MinBlkDelay time.Duration

	// Maximal number of block indexed.
	// Zero signals all blocks are indexed.
	NumHistoricalBlocks uint64

	// Block signer
	StakingLeafSigner crypto.Signer

	// Block certificate
	StakingCertLeaf *staking.Certificate
}

func (c *Config) IsDurangoActivated(timestamp time.Time) bool {
	return !timestamp.Before(c.DurangoTime)
}
