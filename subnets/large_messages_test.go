// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
)

const testMaxMessageSize = 160 * units.MiB

// TestLargeMessagesThrottlerJSON pins the JSON shape of throttlerConfig to the
// example in config.md. The block reuses the throttling package's config
// types, whose keys are nested; this is what catches a doc that drifts from
// the tags.
func TestLargeMessagesThrottlerJSON(t *testing.T) {
	require := require.New(t)

	const doc = `{
	  "maxMessageSize": 167772160,
	  "throttlerConfig": {
	    "inboundMsgThrottlerConfig": {
	      "byteThrottlerConfig": {
	        "atLargeAllocSize": 1073741824,
	        "vdrAllocSize": 1073741824,
	        "nodeMaxAtLargeBytes": 167772160
	      },
	      "bandwidthThrottlerConfig": {
	        "bandwidthRefillRate": 536870912,
	        "bandwidthMaxBurstRate": 167772160
	      },
	      "cpuThrottlerConfig": {"maxRecheckDelay": 5000000000},
	      "diskThrottlerConfig": {"maxRecheckDelay": 5000000000},
	      "maxProcessingMsgsPerNode": 1024
	    },
	    "outboundMsgThrottlerConfig": {
	      "atLargeAllocSize": 4294967296,
	      "vdrAllocSize": 2147483648,
	      "nodeMaxAtLargeBytes": 167772160
	    }
	  }
	}`

	var config LargeMessagesConfig
	require.NoError(json.Unmarshal([]byte(doc), &config))
	require.NoError(config.Verify())

	throttler, err := config.ResolveThrottlerConfig()
	require.NoError(err)
	require.Equal(LargeMessageThrottlerConfig{
		InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
			MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
				AtLargeAllocSize:    units.GiB,
				VdrAllocSize:        units.GiB,
				NodeMaxAtLargeBytes: testMaxMessageSize,
			},
			BandwidthThrottlerConfig: throttling.BandwidthThrottlerConfig{
				RefillRate:   512 * units.MiB,
				MaxBurstSize: testMaxMessageSize,
			},
			CPUThrottlerConfig:       throttling.SystemThrottlerConfig{MaxRecheckDelay: 5 * time.Second},
			DiskThrottlerConfig:      throttling.SystemThrottlerConfig{MaxRecheckDelay: 5 * time.Second},
			MaxProcessingMsgsPerNode: 1024,
		},
		OutboundMsgThrottlerConfig: throttling.MsgByteThrottlerConfig{
			AtLargeAllocSize:    4 * units.GiB,
			VdrAllocSize:        2 * units.GiB,
			NodeMaxAtLargeBytes: testMaxMessageSize,
		},
	}, throttler)
}

// TestLargeMessagesThrottlerDefaults checks that the derived limits keep the
// ratio the default stack has between each limit and its frame size.
func TestLargeMessagesThrottlerDefaults(t *testing.T) {
	require := require.New(t)

	config := LargeMessagesConfig{MaxMessageSize: testMaxMessageSize}
	throttler, err := config.ResolveThrottlerConfig()
	require.NoError(err)
	require.Equal(DefaultLargeMessageThrottlerConfig(testMaxMessageSize), throttler)

	const ratio = testMaxMessageSize / constants.DefaultMaxMessageSize
	inbound, outbound := throttler.InboundMsgThrottlerConfig, throttler.OutboundMsgThrottlerConfig
	require.Equal(uint64(ratio*constants.DefaultInboundThrottlerAtLargeAllocSize), inbound.AtLargeAllocSize)
	require.Equal(uint64(ratio*constants.DefaultInboundThrottlerVdrAllocSize), inbound.VdrAllocSize)
	require.Equal(uint64(testMaxMessageSize), inbound.NodeMaxAtLargeBytes)
	require.Equal(uint64(ratio*constants.DefaultInboundThrottlerBandwidthRefillRate), inbound.RefillRate)
	require.Equal(uint64(testMaxMessageSize), inbound.MaxBurstSize)
	require.Equal(uint64(ratio*constants.DefaultOutboundThrottlerAtLargeAllocSize), outbound.AtLargeAllocSize)
	require.Equal(uint64(ratio*constants.DefaultOutboundThrottlerVdrAllocSize), outbound.VdrAllocSize)
	require.Equal(uint64(testMaxMessageSize), outbound.NodeMaxAtLargeBytes)
}

// TestLargeMessagesThrottlerOverrides checks that a key given under
// throttlerConfig wins and every other key keeps its derived value.
func TestLargeMessagesThrottlerOverrides(t *testing.T) {
	require := require.New(t)

	config := LargeMessagesConfig{
		MaxMessageSize: testMaxMessageSize,
		ThrottlerConfig: json.RawMessage(`{
		  "inboundMsgThrottlerConfig": {
		    "byteThrottlerConfig": {"atLargeAllocSize": 1073741824, "vdrAllocSize": 1073741824},
		    "bandwidthThrottlerConfig": {"bandwidthRefillRate": 536870912},
		    "cpuThrottlerConfig": {"maxRecheckDelay": 7000000000}
		  },
		  "outboundMsgThrottlerConfig": {"atLargeAllocSize": 4294967296, "vdrAllocSize": 2147483648}
		}`),
	}

	throttler, err := config.ResolveThrottlerConfig()
	require.NoError(err)
	var (
		inbound  = throttler.InboundMsgThrottlerConfig
		outbound = throttler.OutboundMsgThrottlerConfig
		defaults = DefaultLargeMessageThrottlerConfig(testMaxMessageSize)
	)
	require.Equal(uint64(units.GiB), inbound.AtLargeAllocSize)
	require.Equal(uint64(units.GiB), inbound.VdrAllocSize)
	require.Equal(uint64(512*units.MiB), inbound.RefillRate)
	require.Equal(7*time.Second, inbound.CPUThrottlerConfig.MaxRecheckDelay)
	require.Equal(uint64(4*units.GiB), outbound.AtLargeAllocSize)
	require.Equal(uint64(2*units.GiB), outbound.VdrAllocSize)

	// Untouched keys keep their derived defaults.
	require.Equal(defaults.InboundMsgThrottlerConfig.NodeMaxAtLargeBytes, inbound.NodeMaxAtLargeBytes)
	require.Equal(defaults.InboundMsgThrottlerConfig.MaxBurstSize, inbound.MaxBurstSize)
	require.Equal(defaults.InboundMsgThrottlerConfig.MaxProcessingMsgsPerNode, inbound.MaxProcessingMsgsPerNode)
	require.Equal(defaults.InboundMsgThrottlerConfig.DiskThrottlerConfig, inbound.DiskThrottlerConfig)
	require.Equal(defaults.OutboundMsgThrottlerConfig.NodeMaxAtLargeBytes, outbound.NodeMaxAtLargeBytes)
}

func TestLargeMessagesVerify(t *testing.T) {
	throttlerConfig := func(doc string) LargeMessagesConfig {
		return LargeMessagesConfig{
			MaxMessageSize:  testMaxMessageSize,
			ThrottlerConfig: json.RawMessage(doc),
		}
	}

	tests := map[string]struct {
		config      LargeMessagesConfig
		expectedErr error
	}{
		"valid": {
			config: LargeMessagesConfig{MaxMessageSize: testMaxMessageSize},
		},
		"size unset": {
			config:      LargeMessagesConfig{},
			expectedErr: ErrLargeMessageSizeTooSmall,
		},
		"size equal to the default": {
			config:      LargeMessagesConfig{MaxMessageSize: constants.DefaultMaxMessageSize},
			expectedErr: ErrLargeMessageSizeTooSmall,
		},
		"unknown key": {
			// A misspelled key used to be ignored, leaving the operator with
			// the derived value and no warning.
			config:      throttlerConfig(`{"inboundMsgThrottlerConfig": {"byteThrottlerConfig": {"atLargeAllocSiz": 1}}}`),
			expectedErr: errInvalidThrottlerConfig,
		},
		"flat key": {
			config:      throttlerConfig(`{"inboundMsgThrottlerConfig": {"atLargeAllocSize": 1073741824}}`),
			expectedErr: errInvalidThrottlerConfig,
		},
		"per-node budget below one frame": {
			config:      throttlerConfig(`{"inboundMsgThrottlerConfig": {"byteThrottlerConfig": {"nodeMaxAtLargeBytes": 167772159}}}`),
			expectedErr: errThrottlerValueTooSmall,
		},
		"outbound per-node budget below one frame": {
			config:      throttlerConfig(`{"outboundMsgThrottlerConfig": {"nodeMaxAtLargeBytes": 167772159}}`),
			expectedErr: errThrottlerValueTooSmall,
		},
		"at-large pool below one frame": {
			// A certificate-only member draws from the at-large pool alone, so
			// a pool this small stalls it on its first large message.
			config:      throttlerConfig(`{"inboundMsgThrottlerConfig": {"byteThrottlerConfig": {"atLargeAllocSize": 167772159}}}`),
			expectedErr: errThrottlerValueTooSmall,
		},
		"outbound at-large pool below one frame": {
			config:      throttlerConfig(`{"outboundMsgThrottlerConfig": {"atLargeAllocSize": 167772159}}`),
			expectedErr: errThrottlerValueTooSmall,
		},
		"validator allocation below one frame is allowed": {
			// VdrAllocSize is drawn second, so a small one costs a validator
			// its head start and nothing more.
			config: throttlerConfig(`{"inboundMsgThrottlerConfig": {"byteThrottlerConfig": {"vdrAllocSize": 167772159}}}`),
		},
		"zero refill rate": {
			config:      throttlerConfig(`{"inboundMsgThrottlerConfig": {"bandwidthThrottlerConfig": {"bandwidthRefillRate": 0}}}`),
			expectedErr: errThrottlerValueZero,
		},
		"zero processing messages": {
			config:      throttlerConfig(`{"inboundMsgThrottlerConfig": {"maxProcessingMsgsPerNode": 0}}`),
			expectedErr: errThrottlerValueZero,
		},
		"recheck delay below the minimum": {
			config:      throttlerConfig(`{"inboundMsgThrottlerConfig": {"diskThrottlerConfig": {"maxRecheckDelay": 999999}}}`),
			expectedErr: errRecheckDelayTooSmall,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.config.Verify()
			if test.expectedErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}
