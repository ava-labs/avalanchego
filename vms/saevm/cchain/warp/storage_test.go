// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

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

			for _, name := range []string{"after_add", "fresh"} {
				t.Run(name, func(t *testing.T) {
					msg, err := s.Get(test.id)
					require.ErrorIsf(t, err, test.wantErr, "%T.Get(%s)", s, test.id)
					require.Equalf(t, test.want, msg, "%T.Get(%s)", s, test.id)
				})
				s = NewStorage(db, test.overrides...)
			}
		})
	}
}
