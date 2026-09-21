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
// types, whose keys are nested, so a flat key is silently ignored and the
// derived default kept; this is what catches a doc that drifts from the tags.
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
	}, config.Throttler())
}

func TestLargeMessagesThrottlerDefaults(t *testing.T) {
	require := require.New(t)

	config := LargeMessagesConfig{MaxMessageSize: testMaxMessageSize}
	throttler := config.Throttler()
	require.Equal(DefaultLargeMessageThrottlerConfig(testMaxMessageSize), throttler)

	// Nothing may be smaller than a single frame, or a peer would stall on its
	// first large message.
	require.GreaterOrEqual(throttler.InboundMsgThrottlerConfig.NodeMaxAtLargeBytes, uint64(testMaxMessageSize))
	require.GreaterOrEqual(throttler.InboundMsgThrottlerConfig.MaxBurstSize, uint64(testMaxMessageSize))
	require.GreaterOrEqual(throttler.OutboundMsgThrottlerConfig.NodeMaxAtLargeBytes, uint64(testMaxMessageSize))
}

// TestLargeMessagesThrottlerOverrides checks that a value given under
// throttlerConfig wins and every other value still defaults from the size.
func TestLargeMessagesThrottlerOverrides(t *testing.T) {
	require := require.New(t)

	config := LargeMessagesConfig{
		MaxMessageSize: testMaxMessageSize,
		ThrottlerConfig: &LargeMessageThrottlerConfig{
			InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
				MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
					AtLargeAllocSize: units.GiB,
					VdrAllocSize:     units.GiB,
				},
				BandwidthThrottlerConfig: throttling.BandwidthThrottlerConfig{
					RefillRate: 512 * units.MiB,
				},
				CPUThrottlerConfig: throttling.SystemThrottlerConfig{
					MaxRecheckDelay: 7 * time.Second,
				},
			},
			OutboundMsgThrottlerConfig: throttling.MsgByteThrottlerConfig{
				AtLargeAllocSize: 4 * units.GiB,
				VdrAllocSize:     2 * units.GiB,
			},
		},
	}

	var (
		throttler = config.Throttler()
		inbound   = throttler.InboundMsgThrottlerConfig
		outbound  = throttler.OutboundMsgThrottlerConfig
		defaults  = DefaultLargeMessageThrottlerConfig(testMaxMessageSize)
	)
	require.Equal(uint64(units.GiB), inbound.AtLargeAllocSize)
	require.Equal(uint64(units.GiB), inbound.VdrAllocSize)
	require.Equal(uint64(512*units.MiB), inbound.RefillRate)
	require.Equal(7*time.Second, inbound.CPUThrottlerConfig.MaxRecheckDelay)
	require.Equal(uint64(4*units.GiB), outbound.AtLargeAllocSize)
	require.Equal(uint64(2*units.GiB), outbound.VdrAllocSize)

	// Untouched values keep their derived defaults.
	require.Equal(defaults.InboundMsgThrottlerConfig.NodeMaxAtLargeBytes, inbound.NodeMaxAtLargeBytes)
	require.Equal(defaults.InboundMsgThrottlerConfig.MaxBurstSize, inbound.MaxBurstSize)
	require.Equal(defaults.InboundMsgThrottlerConfig.MaxProcessingMsgsPerNode, inbound.MaxProcessingMsgsPerNode)
	require.Equal(defaults.InboundMsgThrottlerConfig.DiskThrottlerConfig, inbound.DiskThrottlerConfig)
	require.Equal(defaults.OutboundMsgThrottlerConfig.NodeMaxAtLargeBytes, outbound.NodeMaxAtLargeBytes)
}

func TestLargeMessagesVerify(t *testing.T) {
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
		"per-node budget below one frame": {
			config: LargeMessagesConfig{
				MaxMessageSize: testMaxMessageSize,
				ThrottlerConfig: &LargeMessageThrottlerConfig{
					InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
						MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
							NodeMaxAtLargeBytes: testMaxMessageSize - 1,
						},
					},
				},
			},
			expectedErr: errThrottlerValueTooSmall,
		},
		"outbound per-node budget below one frame": {
			config: LargeMessagesConfig{
				MaxMessageSize: testMaxMessageSize,
				ThrottlerConfig: &LargeMessageThrottlerConfig{
					OutboundMsgThrottlerConfig: throttling.MsgByteThrottlerConfig{
						NodeMaxAtLargeBytes: testMaxMessageSize - 1,
					},
				},
			},
			expectedErr: errThrottlerValueTooSmall,
		},
		"at-large pool below one frame": {
			// A certificate-only member draws from the at-large pool alone, so
			// a pool this small stalls it on its first large message.
			config: LargeMessagesConfig{
				MaxMessageSize: testMaxMessageSize,
				ThrottlerConfig: &LargeMessageThrottlerConfig{
					InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
						MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
							AtLargeAllocSize: testMaxMessageSize - 1,
						},
					},
				},
			},
			expectedErr: errThrottlerValueTooSmall,
		},
		"outbound at-large pool below one frame": {
			config: LargeMessagesConfig{
				MaxMessageSize: testMaxMessageSize,
				ThrottlerConfig: &LargeMessageThrottlerConfig{
					OutboundMsgThrottlerConfig: throttling.MsgByteThrottlerConfig{
						AtLargeAllocSize: testMaxMessageSize - 1,
					},
				},
			},
			expectedErr: errThrottlerValueTooSmall,
		},
		"validator allocation below one frame is allowed": {
			// VdrAllocSize is drawn second, so a small one costs a validator
			// its head start and nothing more.
			config: LargeMessagesConfig{
				MaxMessageSize: testMaxMessageSize,
				ThrottlerConfig: &LargeMessageThrottlerConfig{
					InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
						MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
							VdrAllocSize: testMaxMessageSize - 1,
						},
					},
				},
			},
		},
		"recheck delay below the minimum": {
			config: LargeMessagesConfig{
				MaxMessageSize: testMaxMessageSize,
				ThrottlerConfig: &LargeMessageThrottlerConfig{
					InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
						DiskThrottlerConfig: throttling.SystemThrottlerConfig{
							MaxRecheckDelay: constants.MinInboundThrottlerMaxRecheckDelay - time.Nanosecond,
						},
					},
				},
			},
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
