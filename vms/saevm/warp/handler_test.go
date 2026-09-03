// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestParseOffChainMessages(t *testing.T) {
	msg, _ := newAddressedCall(t)

	tests := []struct {
		name    string
		bytes   []hexutil.Bytes
		want    []*warp.UnsignedMessage
		wantErr error
	}{
		{
			name: "empty",
			want: []*warp.UnsignedMessage{},
		},
		{
			name:  "single_message",
			bytes: []hexutil.Bytes{msg.Bytes()},
			want:  []*warp.UnsignedMessage{msg},
		},
		{
			name:  "multiple_messages",
			bytes: []hexutil.Bytes{msg.Bytes(), msg.Bytes()},
			want:  []*warp.UnsignedMessage{msg, msg},
		},
		{
			name:    "invalid_message",
			bytes:   []hexutil.Bytes{{0xff}},
			wantErr: errParsingWarpMessage,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseOffChainMessages(test.bytes)
			require.ErrorIs(t, err, test.wantErr, "ParseOffChainMessages()")
			require.Equal(t, test.want, got, "ParseOffChainMessages()")
		})
	}
}
