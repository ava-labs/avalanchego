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

	// WindowDuration is the length of a single proposer slot. Lowering it
	// shortens the stall when a scheduled proposer is offline (faster
	// failover), at the cost of more rejected blocks.
	//
	// WARNING: This value is consensus-critical. Every validator of a chain
	// MUST use the same window duration or they disagree about which proposer
	// is expected for a slot, breaking liveness. A value <= 0 falls back to
	// [DefaultWindowDuration] (5s), which preserves historical behavior.
	WindowDuration time.Duration

	// Maximal number of block indexed.
	// Zero signals all blocks are indexed.
	NumHistoricalBlocks uint64

	// Block signer
	StakingLeafSigner crypto.Signer

	// Block certificate
	StakingCertLeaf *staking.Certificate

	// Registerer for prometheus metrics
	Registerer prometheus.Registerer
}
