// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/units"
)

var DefaultConfig = Config{
	MaxValidatorSetStaleness:                    time.Minute,
	TargetGossipSize:                            20 * units.KiB,
	PullGossipPollSize:                          1,
	PullGossipFrequency:                         1500 * time.Millisecond,
	PullGossipThrottlingPeriod:                  10 * time.Second,
	PullGossipThrottlingLimit:                   2,
	ExpectedBloomFilterElements:                 8 * 1024,
	ExpectedBloomFilterFalsePositiveProbability: .01,
	MaxBloomFilterFalsePositiveProbability:      .05,
	LegacyPushGossipCacheSize:                   512,
}

type Config struct {
	// MaxValidatorSetStaleness limits how old of a validator set the network
	// will use for peer sampling and rate limiting.
	MaxValidatorSetStaleness time.Duration `json:"max-validator-set-staleness"`
	// TargetGossipSize is the number of bytes that will be attempted to be
	// sent when pushing transactions and when responded to transaction pull
	// requests.
	TargetGossipSize int `json:"target-gossip-size"`
	// PullGossipPollSize is the number of validators to sample when performing
	// a round of pull gossip.
	PullGossipPollSize int `json:"pull-gossip-poll-size"`
	// PullGossipFrequency is how frequently rounds of pull gossip are
	// performed.
	PullGossipFrequency time.Duration `json:"pull-gossip-frequency"`
	// PullGossipThrottlingPeriod is how large of a window the throttler should
	// use.
	PullGossipThrottlingPeriod time.Duration `json:"pull-gossip-throttling-period"`
	// PullGossipThrottlingLimit is the number of pull querys that are allowed
	// by a validator in every throttling window.
	PullGossipThrottlingLimit int `json:"pull-gossip-throttling-limit"`
	// ExpectedBloomFilterElements is the number of elements to expect when
	// creating a new bloom filter. The larger this number is, the larger the
	// bloom filter will be.
	ExpectedBloomFilterElements int `json:"expected-bloom-filter-elements"`
	// ExpectedBloomFilterFalsePositiveProbability is the expected probability
	// of a false positive after having inserted ExpectedBloomFilterElements
	// into a bloom filter. The smaller this number is, the larger the bloom
	// filter will be.
	ExpectedBloomFilterFalsePositiveProbability float64 `json:"expected-bloom-filter-false-positive-probability"`
	// MaxBloomFilterFalsePositiveProbability is used to determine when the
	// bloom filter should be refreshed. Once the expected probability of a
	// false positive exceeds this value, the bloom filter will be regenerated.
	// The smaller this number is, the more frequently that the bloom filter
	// will be regenerated.
	MaxBloomFilterFalsePositiveProbability float64 `json:"max-bloom-filter-false-positive-probability"`
	// LegacyPushGossipCacheSize tracks the most recently received transactions
	// and ensures to only gossip them once.
	//
	// Deprecated: The legacy push gossip mechanism is deprecated in favor of
	// the p2p SDK's push gossip mechanism.
	LegacyPushGossipCacheSize int `json:"legacy-push-gossip-cache-size"`
}
