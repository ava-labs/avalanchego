// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/units"
)

var DefaultNetwork = Network{
	MaxValidatorSetStaleness:       time.Minute,
	TargetGossipSize:               20 * units.KiB,
	PushGossipPercentStake:         .9,
	PushGossipNumValidators:        100,
	PushGossipNumPeers:             0,
	PushRegossipNumValidators:      10,
	PushRegossipNumPeers:           0,
	PushGossipDiscardedCacheSize:   16384,
	PushGossipMaxRegossipFrequency: 30 * time.Second,
	PushGossipFrequency:            500 * time.Millisecond,
	PullGossipFrequency:            1500 * time.Millisecond,
	PullGossipThrottlingPeriod:     time.Hour,
	// PullGossipRequestsPerValidator = PullGossipThrottlingPeriod / PullGossipFrequency =
	// 3600 seconds/period / 1.5 requests/second = 2400 requests/validator
	PullGossipRequestsPerValidator:              2400,
	ExpectedBloomFilterElements:                 8 * 1024,
	ExpectedBloomFilterFalsePositiveProbability: .01,
	MaxBloomFilterFalsePositiveProbability:      .05,
}

type Network struct {
	// MaxValidatorSetStaleness limits how old of a validator set the network
	// will use for peer sampling and rate limiting.
	MaxValidatorSetStaleness time.Duration `json:"max-validator-set-staleness"`
	// TargetGossipSize is the number of bytes that will be attempted to be
	// sent when pushing transactions and when responded to transaction pull
	// requests.
	TargetGossipSize int `json:"target-gossip-size"`
	// PushGossipPercentStake is the percentage of total stake to push
	// transactions to in the first round of gossip. Nodes with higher stake are
	// preferred over nodes with less stake to minimize the number of messages
	// sent over the p2p network.
	PushGossipPercentStake float64 `json:"push-gossip-percent-stake"`
	// PushGossipNumValidators is the number of validators to push transactions
	// to in the first round of gossip.
	PushGossipNumValidators int `json:"push-gossip-num-validators"`
	// PushGossipNumPeers is the number of peers to push transactions to in the
	// first round of gossip.
	PushGossipNumPeers int `json:"push-gossip-num-peers"`
	// PushRegossipNumValidators is the number of validators to push
	// transactions to after the first round of gossip.
	PushRegossipNumValidators int `json:"push-regossip-num-validators"`
	// PushRegossipNumPeers is the number of peers to push transactions to after
	// the first round of gossip.
	PushRegossipNumPeers int `json:"push-regossip-num-peers"`
	// PushGossipDiscardedCacheSize is the number of txIDs to cache to avoid
	// pushing transactions that were recently dropped from the mempool.
	PushGossipDiscardedCacheSize int `json:"push-gossip-discarded-cache-size"`
	// PushGossipMaxRegossipFrequency is the limit for how frequently a
	// transaction will be push gossiped.
	PushGossipMaxRegossipFrequency time.Duration `json:"push-gossip-max-regossip-frequency"`
	// PushGossipFrequency is how frequently rounds of push gossip are
	// performed.
	PushGossipFrequency time.Duration `json:"push-gossip-frequency"`
	// PullGossipFrequency is how frequently rounds of pull gossip are
	// performed.
	PullGossipFrequency time.Duration `json:"pull-gossip-frequency"`
	// PullGossipThrottlingPeriod is how large of a window the throttler should
	// use.
	PullGossipThrottlingPeriod time.Duration `json:"pull-gossip-throttling-period"`
	// PullGossipRequestsPerValidator is the number of pull gossip requests that
	// a validator is expected to make in a throttling period.
	PullGossipRequestsPerValidator float64 `json:"pull-gossip-requests-per-validator"`
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
}
