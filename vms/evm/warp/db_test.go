// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
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
	db := memdb.New()
	messageDB := NewDB(db)

	require.NoError(t, messageDB.Add(testUnsignedMessage))

	gotMsg, err := messageDB.Get(testUnsignedMessage.ID())
	require.NoError(t, err)
	require.Equal(t, testUnsignedMessage.Bytes(), gotMsg.Bytes())
	require.Equal(t, testUnsignedMessage.ID(), gotMsg.ID())
}

func TestDBGetNotFound(t *testing.T) {
	db := memdb.New()
	messageDB := NewDB(db)

	unknownID := testUnsignedMessage.ID()
	_, err := messageDB.Get(unknownID)
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestDBGetCorruptedData(t *testing.T) {
	db := memdb.New()
	messageDB := NewDB(db)

	corruptedBytes := []byte{0xFF, 0xFF, 0xFF}
	msgID := testUnsignedMessage.ID()
	require.NoError(t, db.Put(msgID[:], corruptedBytes))

	_, err := messageDB.Get(msgID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to parse unsigned message")
}

func TestDBAddDuplicate(t *testing.T) {
	db := memdb.New()
	messageDB := NewDB(db)

	require.NoError(t, messageDB.Add(testUnsignedMessage))
	require.NoError(t, messageDB.Add(testUnsignedMessage))

	gotMsg, err := messageDB.Get(testUnsignedMessage.ID())
	require.NoError(t, err)
	require.Equal(t, testUnsignedMessage.Bytes(), gotMsg.Bytes())
}
