// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

const networkID uint32 = 54321

var sourceChainID = ids.GenerateTestID()

func newHash(tb testing.TB) (*warp.UnsignedMessage, *payload.Hash) {
	p, err := payload.NewHash(
		ids.GenerateTestID(),
	)
	require.NoError(tb, err)

	m, err := warp.NewUnsignedMessage(networkID, sourceChainID, p.Bytes())
	require.NoError(tb, err)
	return m, p
}

func newAddressedCall(tb testing.TB, data []byte) *warp.UnsignedMessage {
	p, err := payload.NewAddressedCall(
		utils.RandomBytes(20),
		data,
	)
	require.NoError(tb, err)

	m, err := warp.NewUnsignedMessage(networkID, sourceChainID, p.Bytes())
	require.NoError(tb, err)
	return m
}

func TestStorage(t *testing.T) {
	msg := newAddressedCall(t, []byte("test"))
	tests := []struct {
		name      string
		overrides []*warp.UnsignedMessage
		add       []*warp.UnsignedMessage
		id        ids.ID
		want      *warp.UnsignedMessage
		wantErr   error
	}{
		{
			name: "add_get",
			add: []*warp.UnsignedMessage{
				msg,
			},
			id:   msg.ID(),
			want: msg,
		},
		{
			name: "get_override",
			overrides: []*warp.UnsignedMessage{
				msg,
			},
			id:   msg.ID(),
			want: msg,
		},
		{
			name:    "get_unknown",
			id:      msg.ID(),
			wantErr: database.ErrNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := memdb.New()
			s := NewStorage(db, test.overrides...)
			for _, msg := range test.add {
				require.NoError(t, s.AddMessage(msg))
			}

			// Verify the message is fetchable.
			msg, err := s.GetMessage(test.id)
			require.ErrorIs(t, err, test.wantErr)
			require.Equal(t, test.want, msg)

			// Verify the message was persisted.
			s = NewStorage(db, test.overrides...)
			msg, err = s.GetMessage(test.id)
			require.ErrorIs(t, err, test.wantErr)
			require.Equal(t, test.want, msg)
		})
	}
}
