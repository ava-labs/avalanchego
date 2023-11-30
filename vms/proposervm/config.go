// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"crypto"
	"time"

	"github.com/ava-labs/avalanchego/staking"
)

type Config struct {
	ActivationTime      time.Time
	DurangoTime         time.Time
	MinimumPChainHeight uint64
	MinBlkDelay         time.Duration
	NumHistoricalBlocks uint64

	// block signer
	StakingLeafSigner crypto.Signer

	// block certificate
	StakingCertLeaf *staking.Certificate
}

func (c *Config) IsDurangoActivated(timestamp time.Time) bool {
	return !timestamp.Before(c.DurangoTime)
}
