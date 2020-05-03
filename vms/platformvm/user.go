// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
)

// Key in the database whose corresponding value is the list of
// account IDs this user controls
var accountIDsKey = ids.Empty.Bytes()

var (
	errDBNil  = errors.New("db uninitialized")
	errKeyNil = errors.New("key uninitialized")
)

type user struct {
	// This user's database, acquired from the keystore
	db database.Database
}

// Get the IDs of the accounts controlled by this user
func (u *user) getAccountIDs() ([]ids.ShortID, error) {
	if u.db == nil {
		return nil, errDBNil
	}

	// If user has no accounts, return empty list
	hasAccounts, err := u.db.Has(accountIDsKey)
	if err != nil {
		return nil, errDB
	}
	if !hasAccounts {
		return nil, nil
	}

	// User has accounts. Get them.
	bytes, err := u.db.Get(accountIDsKey)
	if err != nil {
		return nil, errDB
	}
	accountIDs := []ids.ShortID{}
	if err := Codec.Unmarshal(bytes, &accountIDs); err != nil {
		return nil, err
	}
	return accountIDs, nil
}

// controlsAccount returns true iff this user controls the account
// with the specified ID
func (u *user) controlsAccount(accountID ids.ShortID) (bool, error) {
	if u.db == nil {
		return false, errDBNil
	}
	if accountID.IsZero() {
		return false, errEmptyAccountAddress
	}
	return u.db.Has(accountID.Bytes())
}

// putAccount persists that this user controls the account whose ID is
// [privKey].PublicKey().Address()
func (u *user) putAccount(privKey *crypto.PrivateKeySECP256K1R) error {
	if privKey == nil {
		return errKeyNil
	}

	newAccountID := privKey.PublicKey().Address() // Account the privKey controls
	controlsAccount, err := u.controlsAccount(newAccountID)
	if err != nil {
		return err
	}
	if controlsAccount { // user already controls this account. Do nothing.
		return nil
	}

	err = u.db.Put(newAccountID.Bytes(), privKey.Bytes()) // Account ID --> private key
	if err != nil {
		return errDB
	}

	accountIDs := make([]ids.ShortID, 0) // Add account to list of accounts user controls
	userHasAccounts, err := u.db.Has(accountIDsKey)
	if err != nil {
		return errDB
	}
	if userHasAccounts { // Get accountIDs this user already controls, if they exist
		if accountIDs, err = u.getAccountIDs(); err != nil {
			return errDB
		}
	}
	accountIDs = append(accountIDs, newAccountID)
	bytes, err := Codec.Marshal(accountIDs)
	if err != nil {
		return err
	}
	if err := u.db.Put(accountIDsKey, bytes); err != nil {
		return errDB
	}
	return nil
}

// Key returns the private key that controls the account with the specified ID
func (u *user) getKey(accountID ids.ShortID) (*crypto.PrivateKeySECP256K1R, error) {
	if u.db == nil {
		return nil, errDBNil
	}
	if accountID.IsZero() {
		return nil, errEmptyAccountAddress
	}

	factory := crypto.FactorySECP256K1R{}
	bytes, err := u.db.Get(accountID.Bytes())
	if err != nil {
		return nil, err
	}
	sk, err := factory.ToPrivateKey(bytes)
	if err != nil {
		return nil, err
	}
	if sk, ok := sk.(*crypto.PrivateKeySECP256K1R); ok {
		return sk, nil
	}
	return nil, errDB
}
