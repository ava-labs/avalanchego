// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanche-go/database/memdb"
	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/utils/crypto"
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

func TestUserNilAddress(t *testing.T) {
	u := user{db: memdb.New()}

	_, err := u.controlsAddress(ids.ShortID{})
	assert.Error(t, err, "nil address should have caused an error")

	_, err = u.getKey(ids.ShortID{})
	assert.Error(t, err, "nil address should have caused an error")
}

func TestUser(t *testing.T) {
	u := user{db: memdb.New()}

	addresses, err := u.getAddresses()
	assert.NoError(t, err)
	assert.Empty(t, addresses, "new user shouldn't have address")

	factory := crypto.FactorySECP256K1R{}
	sk, err := factory.NewPrivateKey()
	assert.NoError(t, err)

	err = u.putAddress(sk.(*crypto.PrivateKeySECP256K1R))
	assert.NoError(t, err)

	addr := sk.PublicKey().Address()

	ok, err := u.controlsAddress(addr)
	assert.NoError(t, err)
	assert.True(t, ok, "added address should have been marked as controlled")

	savedSk, err := u.getKey(addr)
	assert.NoError(t, err)
	assert.Equal(t, sk.Bytes(), savedSk.Bytes(), "wrong key returned")

	addresses, err = u.getAddresses()
	assert.NoError(t, err)
	assert.Len(t, addresses, 1, "address should have been added")

	savedAddr := addresses[0]
	equals := addr.Equals(savedAddr)
	assert.True(t, equals, "saved address should match provided address")
}
