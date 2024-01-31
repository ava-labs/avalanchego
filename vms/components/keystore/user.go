// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package keystore

import (
	"fmt"
	"io"

	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/encdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Max number of addresses allowed for a single keystore user
const maxKeystoreAddresses = 5000

var (
	// Key in the database whose corresponding value is the list of addresses
	// this user controls
	addressesKey = ids.Empty[:]

	errMaxAddresses = fmt.Errorf("keystore user has reached its limit of %d addresses", maxKeystoreAddresses)

	_ User = (*user)(nil)
)

type User interface {
	io.Closer

	// Get the addresses controlled by this user
	GetAddresses() ([]ids.ShortID, error)

	// PutKeys persists [privKeys]
	PutKeys(privKeys ...*secp256k1.PrivateKey) error

	// GetKey returns the private key that controls the given address
	GetKey(address ids.ShortID) (*secp256k1.PrivateKey, error)
}

type user struct {
	db *encdb.Database
}

// NewUserFromKeystore tracks a keystore user from the provided keystore
func NewUserFromKeystore(ks keystore.BlockchainKeystore, username, password string) (User, error) {
	db, err := ks.GetDatabase(username, password)
	if err != nil {
		return nil, fmt.Errorf("problem retrieving user %q: %w", username, err)
	}
	return NewUserFromDB(db), nil
}

// NewUserFromDB tracks a keystore user from a database
func NewUserFromDB(db *encdb.Database) User {
	return &user{db: db}
}

func (u *user) GetAddresses() ([]ids.ShortID, error) {
	// Get user's addresses
	addressBytes, err := u.db.Get(addressesKey)
	if err == database.ErrNotFound {
		// If user has no addresses, return empty list
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var addresses []ids.ShortID
	_, err = LegacyCodec.Unmarshal(addressBytes, &addresses)
	return addresses, err
}

func (u *user) PutKeys(privKeys ...*secp256k1.PrivateKey) error {
	toStore := make([]*secp256k1.PrivateKey, 0, len(privKeys))
	for _, privKey := range privKeys {
		address := privKey.PublicKey().Address() // address the privKey controls
		hasAddress, err := u.db.Has(address.Bytes())
		if err != nil {
			return err
		}
		if !hasAddress {
			toStore = append(toStore, privKey)
		}
	}

	// there's nothing to store
	if len(toStore) == 0 {
		return nil
	}

	addresses, err := u.GetAddresses()
	if err != nil {
		return err
	}

	if len(toStore) > maxKeystoreAddresses || len(addresses) > maxKeystoreAddresses-len(toStore) {
		return errMaxAddresses
	}

	for _, privKey := range toStore {
		address := privKey.PublicKey().Address() // address the privKey controls
		// Address --> private key
		if err := u.db.Put(address.Bytes(), privKey.Bytes()); err != nil {
			return err
		}
		addresses = append(addresses, address)
	}

	addressBytes, err := Codec.Marshal(CodecVersion, addresses)
	if err != nil {
		return err
	}
	return u.db.Put(addressesKey, addressBytes)
}

func (u *user) GetKey(address ids.ShortID) (*secp256k1.PrivateKey, error) {
	bytes, err := u.db.Get(address.Bytes())
	if err != nil {
		return nil, err
	}
	return secp256k1.ToPrivateKey(bytes)
}

func (u *user) Close() error {
	return u.db.Close()
}

// Create and store a new key that will be controlled by this user.
func NewKey(u User) (*secp256k1.PrivateKey, error) {
	keys, err := NewKeys(u, 1)
	if err != nil {
		return nil, err
	}
	return keys[0], nil
}

// Create and store [numKeys] new keys that will be controlled by this user.
func NewKeys(u User, numKeys int) ([]*secp256k1.PrivateKey, error) {
	keys := make([]*secp256k1.PrivateKey, numKeys)
	for i := range keys {
		sk, err := secp256k1.NewPrivateKey()
		if err != nil {
			return nil, err
		}
		keys[i] = sk
	}
	return keys, u.PutKeys(keys...)
}

// Keychain returns a new keychain from the [user].
// If [addresses] is non-empty it fetches only the keys in addresses. If a key
// is missing, it will be ignored.
// If [addresses] is empty, then it will create a keychain using every address
// in the provided [user].
func GetKeychain(u User, addresses set.Set[ids.ShortID]) (*secp256k1fx.Keychain, error) {
	addrsList := addresses.List()
	if len(addrsList) == 0 {
		var err error
		addrsList, err = u.GetAddresses()
		if err != nil {
			return nil, err
		}
	}

	kc := secp256k1fx.NewKeychain()
	for _, addr := range addrsList {
		sk, err := u.GetKey(addr)
		if err == database.ErrNotFound {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("problem retrieving private key for address %s: %w", addr, err)
		}
		kc.Add(sk)
	}
	return kc, nil
}
