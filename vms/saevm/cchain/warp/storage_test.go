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
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var sourceChainID = ids.GenerateTestID()

func newHash(tb testing.TB) (*warp.UnsignedMessage, *payload.Hash) {
	p, err := payload.NewHash(
		ids.GenerateTestID(),
	)
	require.NoError(tb, err, "payload.NewHash()")

	m, err := warp.NewUnsignedMessage(constants.UnitTestID, sourceChainID, p.Bytes())
	require.NoError(tb, err, "warp.NewUnsignedMessage()")
	return m, p
}

func newAddressedCall(tb testing.TB) (*warp.UnsignedMessage, *payload.AddressedCall) {
	p, err := payload.NewAddressedCall(
		utils.RandomBytes(20),
		[]byte("test"),
	)
	require.NoError(tb, err, "payload.NewAddressedCall()")

	m, err := warp.NewUnsignedMessage(constants.UnitTestID, sourceChainID, p.Bytes())
	require.NoError(tb, err, "warp.NewUnsignedMessage()")
	return m, p
}

func TestStorage(t *testing.T) {
	msg, _ := newAddressedCall(t)
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
			require.NoErrorf(t, s.Add(test.add...), "%T.Add(%d)", s, len(test.add))

			msg, err := s.Get(test.id)
			require.ErrorIsf(t, err, test.wantErr, "%T.Get(%s)", s, test.id)
			require.Equalf(t, test.want, msg, "%T.Get(%s)", s, test.id)

			// Verify the message was persisted.
			s = NewStorage(db, test.overrides...)
			msg, err = s.Get(test.id)
			require.ErrorIsf(t, err, test.wantErr, "reloaded %T.Get(%s)", s, test.id)
			require.Equalf(t, test.want, msg, "reloaded %T.Get(%s)", s, test.id)
		})
	}
}
