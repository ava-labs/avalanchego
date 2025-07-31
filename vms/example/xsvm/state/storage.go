// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	errWrongNonce          = errors.New("wrong nonce")
	errInsufficientBalance = errors.New("insufficient balance")
)

/*
 * VMDB
 * |-- initializedKey -> nil
 * |-. blocks
 * | |-- lastAcceptedKey -> blockID
 * | |-- height -> blockID
 * | '-- blockID -> block bytes
 * |-. addresses
 * | '-- addressID -> nonce
 * | '-- addressID + chainID -> balance
 * |-. chains
 * | |-- chainID -> balance
 * | '-- chainID + loanID -> nil
 * '-. message
 *   '-- txID -> message bytes
 */

// Chain state

func IsInitialized(db database.KeyValueReader) (bool, error) {
	return db.Has(initializedKey)
}

func SetInitialized(db database.KeyValueWriter) error {
	return db.Put(initializedKey, nil)
}

// Block state

func GetLastAccepted(db database.KeyValueReader) (ids.ID, error) {
	return database.GetID(db, blockPrefix)
}

func SetLastAccepted(db database.KeyValueWriter, blkID ids.ID) error {
	return database.PutID(db, blockPrefix, blkID)
}

func GetBlockIDByHeight(db database.KeyValueReader, height uint64) (ids.ID, error) {
	key := Flatten(blockPrefix, database.PackUInt64(height))
	return database.GetID(db, key)
}

func GetBlock(db database.KeyValueReader, blkID ids.ID) ([]byte, error) {
	key := Flatten(blockPrefix, blkID[:])
	return db.Get(key)
}

func AddBlock(db database.KeyValueWriter, height uint64, blkID ids.ID, blk []byte) error {
	heightToIDKey := Flatten(blockPrefix, database.PackUInt64(height))
	if err := database.PutID(db, heightToIDKey, blkID); err != nil {
		return err
	}
	idToBlockKey := Flatten(blockPrefix, blkID[:])
	return db.Put(idToBlockKey, blk)
}

// Address state

func GetNonce(db database.KeyValueReader, address ids.ShortID) (uint64, error) {
	key := Flatten(addressPrefix, address[:])
	return database.WithDefault(database.GetUInt64, db, key, 0)
}

func SetNonce(db database.KeyValueWriter, address ids.ShortID, nonce uint64) error {
	key := Flatten(addressPrefix, address[:])
	return database.PutUInt64(db, key, nonce)
}

func IncrementNonce(db database.KeyValueReaderWriter, address ids.ShortID, nonce uint64) error {
	expectedNonce, err := GetNonce(db, address)
	if err != nil {
		return err
	}
	if nonce != expectedNonce {
		return errWrongNonce
	}
	return SetNonce(db, address, nonce+1)
}

func GetBalance(db database.KeyValueReader, address ids.ShortID, chainID ids.ID) (uint64, error) {
	key := Flatten(addressPrefix, address[:], chainID[:])
	return database.WithDefault(database.GetUInt64, db, key, 0)
}

func SetBalance(db database.KeyValueWriterDeleter, address ids.ShortID, chainID ids.ID, balance uint64) error {
	key := Flatten(addressPrefix, address[:], chainID[:])
	if balance == 0 {
		return db.Delete(key)
	}
	return database.PutUInt64(db, key, balance)
}

func DecreaseBalance(db database.KeyValueReaderWriterDeleter, address ids.ShortID, chainID ids.ID, amount uint64) error {
	balance, err := GetBalance(db, address, chainID)
	if err != nil {
		return err
	}
	if balance < amount {
		return errInsufficientBalance
	}
	return SetBalance(db, address, chainID, balance-amount)
}

func IncreaseBalance(db database.KeyValueReaderWriterDeleter, address ids.ShortID, chainID ids.ID, amount uint64) error {
	balance, err := GetBalance(db, address, chainID)
	if err != nil {
		return err
	}
	balance, err = math.Add(balance, amount)
	if err != nil {
		return err
	}
	return SetBalance(db, address, chainID, balance)
}

// Chain state

func HasLoanID(db database.KeyValueReader, chainID ids.ID, loanID ids.ID) (bool, error) {
	key := Flatten(chainPrefix, chainID[:], loanID[:])
	return db.Has(key)
}

func AddLoanID(db database.KeyValueWriter, chainID ids.ID, loanID ids.ID) error {
	key := Flatten(chainPrefix, chainID[:], loanID[:])
	return db.Put(key, nil)
}

func GetLoan(db database.KeyValueReader, chainID ids.ID) (uint64, error) {
	key := Flatten(chainPrefix, chainID[:])
	return database.WithDefault(database.GetUInt64, db, key, 0)
}

func SetLoan(db database.KeyValueWriterDeleter, chainID ids.ID, balance uint64) error {
	key := Flatten(chainPrefix, chainID[:])
	if balance == 0 {
		return db.Delete(key)
	}
	return database.PutUInt64(db, key, balance)
}

func DecreaseLoan(db database.KeyValueReaderWriterDeleter, chainID ids.ID, amount uint64) error {
	balance, err := GetLoan(db, chainID)
	if err != nil {
		return err
	}
	if balance < amount {
		return errInsufficientBalance
	}
	return SetLoan(db, chainID, balance-amount)
}

func IncreaseLoan(db database.KeyValueReaderWriterDeleter, chainID ids.ID, amount uint64) error {
	balance, err := GetLoan(db, chainID)
	if err != nil {
		return err
	}
	balance, err = math.Add(balance, amount)
	if err != nil {
		return err
	}
	return SetLoan(db, chainID, balance)
}

// Message state

func GetMessage(db database.KeyValueReader, txID ids.ID) (*warp.UnsignedMessage, error) {
	key := Flatten(messagePrefix, txID[:])
	bytes, err := db.Get(key)
	if err != nil {
		return nil, err
	}
	return warp.ParseUnsignedMessage(bytes)
}

func SetMessage(db database.KeyValueWriter, txID ids.ID, message *warp.UnsignedMessage) error {
	key := Flatten(messagePrefix, txID[:])
	bytes := message.Bytes()
	return db.Put(key, bytes)
}
