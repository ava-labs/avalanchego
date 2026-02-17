// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestDBAddAndSign(t *testing.T) {
	db := memdb.New()

	sk, err := localsigner.New()
	require.NoError(t, err)
	warpSigner := warp.NewSigner(sk, networkID, sourceChainID)

	messageDB := NewDB(db)

	require.NoError(t, messageDB.Add(testUnsignedMessage))

	signature, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)

	wantSig, err := warpSigner.Sign(testUnsignedMessage)
	require.NoError(t, err)
	require.Equal(t, wantSig, signature)
}

func TestDBGet(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(testing.TB, database.Database, *DB)
		queryID func() ids.ID
		wantMsg *warp.UnsignedMessage
		wantErr error
	}{
		{
			name: "existing message",
			setup: func(t testing.TB, _ database.Database, db *DB) {
				require.NoError(t, db.Add(testUnsignedMessage))
			},
			queryID: testUnsignedMessage.ID,
			wantMsg: testUnsignedMessage,
		},
		{
			name:    "not found",
			setup:   func(testing.TB, database.Database, *DB) {},
			queryID: testUnsignedMessage.ID,
			wantErr: database.ErrNotFound,
		},
		{
			name: "corrupted data",
			setup: func(t testing.TB, rawDB database.Database, _ *DB) {
				msgID := testUnsignedMessage.ID()
				require.NoError(t, rawDB.Put(msgID[:], []byte{0xFF, 0xFF, 0xFF}))
			},
			queryID: testUnsignedMessage.ID,
			wantErr: codec.ErrUnknownVersion,
		},
		{
			name: "duplicate add",
			setup: func(t testing.TB, _ database.Database, db *DB) {
				require.NoError(t, db.Add(testUnsignedMessage))
				require.NoError(t, db.Add(testUnsignedMessage))
			},
			queryID: testUnsignedMessage.ID,
			wantMsg: testUnsignedMessage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawDB := memdb.New()
			messageDB := NewDB(rawDB)
			tt.setup(t, rawDB, messageDB)

			got, err := messageDB.Get(tt.queryID())

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantMsg.Bytes(), got.Bytes())
			require.Equal(t, tt.wantMsg.ID(), got.ID())
		})
	}
}
