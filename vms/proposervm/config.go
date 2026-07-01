// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"crypto"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/upgrade"
)

type Config struct {
	Upgrades upgrade.Config

	// Configurable minimal delay among blocks issued consecutively
	MinBlkDelay time.Duration

	// Maximal number of block indexed.
	// Zero signals all blocks are indexed.
	NumHistoricalBlocks uint64

	// Block signer
	StakingLeafSigner crypto.Signer

	// Block certificate
	StakingCertLeaf *staking.Certificate

	// Registerer for prometheus metrics
	Registerer prometheus.Registerer

	FallbackNonValidatorCanPropose bool
	FallbackProposerMaxWaitTime    time.Duration
}
