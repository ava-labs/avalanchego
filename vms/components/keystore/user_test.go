// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/encdb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
)

// Test user password, must meet minimum complexity/length requirements
const testPassword = "ShaggyPassword1Zoinks!"

func TestUserClosedDB(t *testing.T) {
	require := require.New(t)

	db, err := encdb.New([]byte(testPassword), memdb.New())
	require.NoError(err)

	require.NoError(db.Close())

	u := NewUserFromDB(db)

	_, err = u.GetAddresses()
	require.ErrorIs(err, database.ErrClosed)

	_, err = u.GetKey(ids.ShortEmpty)
	require.ErrorIs(err, database.ErrClosed)

	_, err = GetKeychain(u, nil)
	require.ErrorIs(err, database.ErrClosed)

	sk, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	err = u.PutKeys(sk)
	require.ErrorIs(err, database.ErrClosed)
}

func TestUser(t *testing.T) {
	require := require.New(t)

	db, err := encdb.New([]byte(testPassword), memdb.New())
	require.NoError(err)

	u := NewUserFromDB(db)

	addresses, err := u.GetAddresses()
	require.NoError(err)
	require.Empty(addresses, "new user shouldn't have address")

	sk, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	require.NoError(u.PutKeys(sk))

	// Putting the same key multiple times should be a noop
	require.NoError(u.PutKeys(sk))

	addr := sk.PublicKey().Address()

	savedSk, err := u.GetKey(addr)
	require.NoError(err)
	require.Equal(sk.Bytes(), savedSk.Bytes(), "wrong key returned")

	addresses, err = u.GetAddresses()
	require.NoError(err)
	require.Len(addresses, 1, "address should have been added")

	savedAddr := addresses[0]
	require.Equal(addr, savedAddr, "saved address should match provided address")

	savedKeychain, err := GetKeychain(u, nil)
	require.NoError(err)
	require.Len(savedKeychain.Keys, 1, "key should have been added")
	require.Equal(sk.Bytes(), savedKeychain.Keys[0].Bytes(), "wrong key returned")
}
