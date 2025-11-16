// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

// TestCodecSerialization tests the registration order changes in codec.go,
// does not change, preventing unintended serialization format changes.
func TestCodecSerialization(t *testing.T) {
	tests := []struct {
		name      string
		msg       *ValidatorUptime
		wantBytes []byte
	}{
		{
			name: "zero values",
			msg: &ValidatorUptime{
				ValidationID: ids.Empty,
				TotalUptime:  0,
			},
			wantBytes: []byte{
				// Codec version (0)
				0x00, 0x00,
				// ValidationID (32 bytes of zeros)
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// TotalUptime (8 bytes, uint64 = 0)
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
		},
		{
			name: "non-zero values",
			msg: &ValidatorUptime{
				ValidationID: ids.ID{
					0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
					0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
					0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
					0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				},
				TotalUptime: 12345,
			},
			wantBytes: []byte{
				// Codec version (0)
				0x00, 0x00,
				// ValidationID (32 bytes)
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
				// TotalUptime (8 bytes, uint64 = 12345 in big-endian)
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x30, 0x39,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// Test marshaling produces expected bytes
			gotBytes, err := Codec.Marshal(CodecVersion, tt.msg)
			require.NoError(err)
			require.Equal(tt.wantBytes, gotBytes, "marshaled bytes do not match expected - codec registration order may have changed")

			// Test unmarshaling the expected bytes produces the original message
			var gotMsg ValidatorUptime
			version, err := Codec.Unmarshal(tt.wantBytes, &gotMsg)
			require.NoError(err)
			require.Equal(uint16(CodecVersion), version)
			require.Equal(tt.msg.ValidationID, gotMsg.ValidationID)
			require.Equal(tt.msg.TotalUptime, gotMsg.TotalUptime)
		})
	}
}

// TestCodecRoundTrip verifies that messages can be marshaled and unmarshaled
// without data loss.
func TestCodecRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  *ValidatorUptime
	}{
		{
			name: "zero values",
			msg: &ValidatorUptime{
				ValidationID: ids.Empty,
				TotalUptime:  0,
			},
		},
		{
			name: "max uptime",
			msg: &ValidatorUptime{
				ValidationID: ids.GenerateTestID(),
				TotalUptime:  ^uint64(0), // max uint64
			},
		},
		{
			name: "random values",
			msg: &ValidatorUptime{
				ValidationID: ids.GenerateTestID(),
				TotalUptime:  987654321,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bytes, err := Codec.Marshal(CodecVersion, tt.msg)
			require.NoError(t, err)

			var gotMsg ValidatorUptime
			version, err := Codec.Unmarshal(bytes, &gotMsg)
			require.NoError(t, err)
			require.Equal(t, uint16(CodecVersion), version)
			require.Equal(t, tt.msg.ValidationID, gotMsg.ValidationID)
			require.Equal(t, tt.msg.TotalUptime, gotMsg.TotalUptime)
		})
	}
}
