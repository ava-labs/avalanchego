// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/chain4travel/caminogo/database/encdb"
	"github.com/chain4travel/caminogo/database/memdb"
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils/crypto"
)

// Test user password, must meet minimum complexity/length requirements
const testPassword = "ShaggyPassword1Zoinks!"

func TestUserClosedDB(t *testing.T) {
	assert := assert.New(t)

	db, err := encdb.New([]byte(testPassword), memdb.New())
	assert.NoError(err)

	err = db.Close()
	assert.NoError(err)

	u := NewUserFromDB(db)

	_, err = u.GetAddresses()
	assert.Error(err, "closed db should have caused an error")

	_, err = u.GetKey(ids.ShortEmpty)
	assert.Error(err, "closed db should have caused an error")

	_, err = GetKeychain(u, nil)
	assert.Error(err, "closed db should have caused an error")

	factory := crypto.FactorySECP256K1R{}
	sk, err := factory.NewPrivateKey()
	assert.NoError(err)

	err = u.PutKeys(sk.(*crypto.PrivateKeySECP256K1R))
	assert.Error(err, "closed db should have caused an error")
}

func TestUser(t *testing.T) {
	assert := assert.New(t)

	db, err := encdb.New([]byte(testPassword), memdb.New())
	assert.NoError(err)

	u := NewUserFromDB(db)

	addresses, err := u.GetAddresses()
	assert.NoError(err)
	assert.Empty(addresses, "new user shouldn't have address")

	factory := crypto.FactorySECP256K1R{}
	sk, err := factory.NewPrivateKey()
	assert.NoError(err)

	err = u.PutKeys(sk.(*crypto.PrivateKeySECP256K1R))
	assert.NoError(err)

	// Putting the same key multiple times should be a noop
	err = u.PutKeys(sk.(*crypto.PrivateKeySECP256K1R))
	assert.NoError(err)

	addr := sk.PublicKey().Address()

	savedSk, err := u.GetKey(addr)
	assert.NoError(err)
	assert.Equal(sk.Bytes(), savedSk.Bytes(), "wrong key returned")

	addresses, err = u.GetAddresses()
	assert.NoError(err)
	assert.Len(addresses, 1, "address should have been added")

	savedAddr := addresses[0]
	assert.Equal(addr, savedAddr, "saved address should match provided address")

	savedKeychain, err := GetKeychain(u, nil)
	assert.NoError(err)
	assert.Len(savedKeychain.Keys, 1, "key should have been added")
	assert.Equal(sk.Bytes(), savedKeychain.Keys[0].Bytes(), "wrong key returned")
}
