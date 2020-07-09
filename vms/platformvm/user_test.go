// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
)

func TestUserNilDB(t *testing.T) {
	u := user{}

	_, err := u.getAddresses()
	assert.Error(t, err, "nil db should have caused an error")

	_, err = u.controlsAddress(ids.ShortEmpty)
	assert.Error(t, err, "nil db should have caused an error")

	_, err = u.getKey(ids.ShortEmpty)
	assert.Error(t, err, "nil db should have caused an error")

	_, err = u.getKeys()
	assert.Error(t, err, "nil db should have caused an error")

	factory := crypto.FactorySECP256K1R{}
	sk, err := factory.NewPrivateKey()
	assert.NoError(t, err)

	err = u.putAddress(sk.(*crypto.PrivateKeySECP256K1R))
	assert.Error(t, err, "nil db should have caused an error")
}

func TestUserClosedDB(t *testing.T) {
	db := memdb.New()
	err := db.Close()
	assert.NoError(t, err)

	u := user{db}

	_, err = u.getAddresses()
	assert.Error(t, err, "closed db should have caused an error")

	_, err = u.controlsAddress(ids.ShortEmpty)
	assert.Error(t, err, "closed db should have caused an error")

	_, err = u.getKey(ids.ShortEmpty)
	assert.Error(t, err, "closed db should have caused an error")

	_, err = u.getKeys()
	assert.Error(t, err, "closed db should have caused an error")

	factory := crypto.FactorySECP256K1R{}
	sk, err := factory.NewPrivateKey()
	assert.NoError(t, err)

	err = u.putAddress(sk.(*crypto.PrivateKeySECP256K1R))
	assert.Error(t, err, "closed db should have caused an error")
}

func TestUserNilSK(t *testing.T) {
	u := user{db: memdb.New()}

	err := u.putAddress(nil)
	assert.Error(t, err, "nil key should have caused an error")
}

func TestUserNilAccount(t *testing.T) {
	u := user{db: memdb.New()}

	_, err := u.controlsAddress(ids.ShortID{})
	assert.Error(t, err, "nil accountID should have caused an error")

	_, err = u.getKey(ids.ShortID{})
	assert.Error(t, err, "nil accountID should have caused an error")
}

func TestUser(t *testing.T) {
	u := user{db: memdb.New()}

	accountIDs, err := u.getAddresses()
	assert.NoError(t, err)
	assert.Empty(t, accountIDs, "new user shouldn't have accounts")

	factory := crypto.FactorySECP256K1R{}
	sk, err := factory.NewPrivateKey()
	assert.NoError(t, err)

	err = u.putAddress(sk.(*crypto.PrivateKeySECP256K1R))
	assert.NoError(t, err)

	addr := sk.PublicKey().Address()

	ok, err := u.controlsAddress(addr)
	assert.NoError(t, err)
	assert.True(t, ok, "added account should have been marked as controlled")

	savedSk, err := u.getKey(addr)
	assert.NoError(t, err)
	assert.Equal(t, sk.Bytes(), savedSk.Bytes(), "wrong key returned")

	accountIDs, err = u.getAddresses()
	assert.NoError(t, err)
	assert.Len(t, accountIDs, 1, "account should have been added")

	savedAddr := accountIDs[0]
	equals := addr.Equals(savedAddr)
	assert.True(t, equals, "saved address should match provided address")
}
