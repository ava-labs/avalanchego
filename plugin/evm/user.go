// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database/encdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ethereum/go-ethereum/common"
)

// Key in the database whose corresponding value is the list of
// addresses this user controls
var addressesKey = ids.Empty[:]

var (
	errDBNil  = errors.New("db uninitialized")
	errKeyNil = errors.New("key uninitialized")
)

type user struct {
	secpFactory *crypto.FactorySECP256K1R
	// This user's database, acquired from the keystore
	db *encdb.Database
}

// Get the addresses controlled by this user
func (u *user) getAddresses() ([]common.Address, error) {
	if u.db == nil {
		return nil, errDBNil
	}

	// If user has no addresses, return empty list
	hasAddresses, err := u.db.Has(addressesKey)
	if err != nil {
		return nil, err
	}
	if !hasAddresses {
		return nil, nil
	}

	// User has addresses. Get them.
	bytes, err := u.db.Get(addressesKey)
	if err != nil {
		return nil, err
	}
	addresses := []common.Address{}
	if _, err := Codec.Unmarshal(bytes, &addresses); err != nil {
		return nil, err
	}
	return addresses, nil
}

// controlsAddress returns true iff this user controls the given address
func (u *user) controlsAddress(address common.Address) (bool, error) {
	if u.db == nil {
		return false, errDBNil
		//} else if address.IsZero() {
		//	return false, errEmptyAddress
	}
	return u.db.Has(address.Bytes())
}

// putAddress persists that this user controls address controlled by [privKey]
func (u *user) putAddress(privKey *crypto.PrivateKeySECP256K1R) error {
	if privKey == nil {
		return errKeyNil
	}

	address := GetEthAddress(privKey) // address the privKey controls
	controlsAddress, err := u.controlsAddress(address)
	if err != nil {
		return err
	}
	if controlsAddress { // user already controls this address. Do nothing.
		return nil
	}

	if err := u.db.Put(address.Bytes(), privKey.Bytes()); err != nil { // Address --> private key
		return err
	}

	addresses := make([]common.Address, 0) // Add address to list of addresses user controls
	userHasAddresses, err := u.db.Has(addressesKey)
	if err != nil {
		return err
	}
	if userHasAddresses { // Get addresses this user already controls, if they exist
		if addresses, err = u.getAddresses(); err != nil {
			return err
		}
	}
	addresses = append(addresses, address)
	bytes, err := Codec.Marshal(codecVersion, addresses)
	if err != nil {
		return err
	}
	if err := u.db.Put(addressesKey, bytes); err != nil {
		return err
	}
	return nil
}

// Key returns the private key that controls the given address
func (u *user) getKey(address common.Address) (*crypto.PrivateKeySECP256K1R, error) {
	if u.db == nil {
		return nil, errDBNil
		//} else if address.IsZero() {
		//	return nil, errEmptyAddress
	}

	bytes, err := u.db.Get(address.Bytes())
	if err != nil {
		return nil, err
	}
	sk, err := u.secpFactory.ToPrivateKey(bytes)
	if err != nil {
		return nil, err
	}
	if sk, ok := sk.(*crypto.PrivateKeySECP256K1R); ok {
		return sk, nil
	}
	return nil, fmt.Errorf("expected private key to be type *crypto.PrivateKeySECP256K1R but is type %T", sk)
}

// Return all private keys controlled by this user
func (u *user) getKeys() ([]*crypto.PrivateKeySECP256K1R, error) {
	addrs, err := u.getAddresses()
	if err != nil {
		return nil, err
	}
	keys := make([]*crypto.PrivateKeySECP256K1R, len(addrs))
	for i, addr := range addrs {
		key, err := u.getKey(addr)
		if err != nil {
			return nil, err
		}
		keys[i] = key
	}
	return keys, nil
}
