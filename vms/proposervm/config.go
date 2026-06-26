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

	// WindowDuration is the length of a single proposer slot. Consensus-critical;
	// see subnets.Config.ProposerWindowMilliseconds. A value <= 0 falls back to
	// [DefaultWindowDuration] (5s).
	WindowDuration time.Duration

	// Maximal number of block indexed.
	// Zero signals all blocks are indexed.
	NumHistoricalBlocks uint64

	// MillisecondTimestamps interprets the wrapper block's int64 timestamp as
	// unix-milliseconds instead of unix-seconds, letting sub-second proposer
	// windows actually advance. It is a per-chain consensus parameter and MUST
	// be identical across all of the chain's validators and fixed from genesis;
	// flipping it on a chain with second-granular history misreads old blocks.
	// Defaults to false (seconds), so existing networks are unchanged.
	MillisecondTimestamps bool

	// Block signer
	StakingLeafSigner crypto.Signer

	// Block certificate
	StakingCertLeaf *staking.Certificate

	// Registerer for prometheus metrics
	Registerer prometheus.Registerer
}
