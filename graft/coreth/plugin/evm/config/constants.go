// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	TxGossipBloomMinTargetElements       = 8 * 1024
	TxGossipBloomTargetFalsePositiveRate = 0.01
	TxGossipBloomResetFalsePositiveRate  = 0.05
	TxGossipBloomChurnMultiplier         = 3
	PushGossipDiscardedElements          = 16_384
	TxGossipTargetMessageSize            = 20 * units.KiB
	TxGossipThrottlingPeriod             = 10 * time.Second
	TxGossipThrottlingLimit              = 2
	TxGossipPollSize                     = 1
)
