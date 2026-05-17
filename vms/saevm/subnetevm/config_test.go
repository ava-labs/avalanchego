// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

func TestParseConfig_FeeRecipient(t *testing.T) {
	tests := []struct {
		name    string
		bytes   []byte
		want    string
		wantErr error
	}{
		{
			name:  "empty_bytes",
			bytes: nil,
			want:  "",
		},
		{
			name:  "absent_field",
			bytes: []byte(`{}`),
			want:  "",
		},
		{
			name:  "explicit_empty_string",
			bytes: []byte(`{"feeRecipient":""}`),
			want:  "",
		},
		{
			name:  "valid_hex_address_with_prefix",
			bytes: []byte(`{"feeRecipient":"0x0123456789abcdef0123456789abcdef01234567"}`),
			want:  "0x0123456789abcdef0123456789abcdef01234567",
		},
		{
			name:  "valid_hex_address_without_prefix",
			bytes: []byte(`{"feeRecipient":"0123456789abcdef0123456789abcdef01234567"}`),
			want:  "0123456789abcdef0123456789abcdef01234567",
		},
		{
			name:    "invalid_hex_address_too_short",
			bytes:   []byte(`{"feeRecipient":"0xdead"}`),
			wantErr: errInvalidFeeRecipient,
		},
		{
			name:    "invalid_hex_address_garbage",
			bytes:   []byte(`{"feeRecipient":"not-an-address"}`),
			wantErr: errInvalidFeeRecipient,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := ParseConfig(test.bytes)
			require.ErrorIs(t, err, test.wantErr)
			if test.wantErr == nil {
				require.Equal(t, test.want, c.FeeRecipient)
			}
		})
	}
}

func TestConfig_WarpMessages(t *testing.T) {
	payload, err := payload.NewAddressedCall(
		utils.RandomBytes(20),
		[]byte("test"),
	)
	require.NoError(t, err)

	msg, err := warp.NewUnsignedMessage(12345, ids.GenerateTestID(), payload.Bytes())
	require.NoError(t, err)

	tests := []struct {
		name    string
		bytes   [][]byte
		want    []*warp.UnsignedMessage
		wantErr error
	}{
		{
			name: "empty",
			want: []*warp.UnsignedMessage{},
		},
		{
			name:  "single_message",
			bytes: [][]byte{msg.Bytes()},
			want:  []*warp.UnsignedMessage{msg},
		},
		{
			name:  "multiple_messages",
			bytes: [][]byte{msg.Bytes(), msg.Bytes()},
			want:  []*warp.UnsignedMessage{msg, msg},
		},
		{
			name:    "invalid_message",
			bytes:   [][]byte{{0xff}},
			wantErr: errParsingWarpMessage,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var c Config
			for _, msgBytes := range test.bytes {
				c.WarpOffChainMessages = append(c.WarpOffChainMessages, msgBytes)
			}

			got, err := c.WarpMessages()
			require.ErrorIs(t, err, test.wantErr)
			require.Equal(t, test.want, got)
		})
	}
}
