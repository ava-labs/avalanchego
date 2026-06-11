// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"encoding/json"
	"testing"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    Config
		wantErr testerr.Want
	}{
		{
			name:    "invalid_json",
			json:    "invalid",
			wantErr: testerr.As(func(*json.SyntaxError) string { return "" }),
		},
		{
			name: "empty_input",
		},
		{
			name: "empty_object",
			json: `{}`,
		},
		{
			name: "warp_off_chain_messages",
			json: `{"warp-off-chain-messages":["0x1234"]}`,
			want: Config{
				WarpOffChainMessages: []hexutil.Bytes{{0x12, 0x34}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseConfig([]byte(test.json))
			if diff := testerr.Diff(err, test.wantErr); diff != "" {
				t.Errorf("ParseConfig(%s) error (-want +got)\n%s", test.json, diff)
			}
			require.Equalf(t, test.want, got, "ParseConfig(%s)", test.json)
		})
	}
}

func TestConfig_WarpMessages(t *testing.T) {
	payload, err := payload.NewAddressedCall(
		utils.RandomBytes(20),
		[]byte("test"),
	)
	require.NoError(t, err)

	msg, err := warp.NewUnsignedMessage(constants.UnitTestID, ids.GenerateTestID(), payload.Bytes())
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
			c := Config{
				WarpOffChainMessages: make([]hexutil.Bytes, len(test.bytes)),
			}
			for i, msgBytes := range test.bytes {
				c.WarpOffChainMessages[i] = msgBytes
			}

			got, err := c.WarpMessages()
			require.ErrorIs(t, err, test.wantErr)
			require.Equal(t, test.want, got)
		})
	}
}
