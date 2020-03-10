// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
)

var (
	errNilChainID = errors.New("nil chain id")
)

// Builder defines the functionality for building payment objects.
type Builder struct {
	NetworkID uint32
	ChainID   ids.ID
}

// NewAccount creates a new Account
func (b Builder) NewAccount(id ids.ShortID, nonce, balance uint64) Account {
	return Account{
		id:      id,
		nonce:   nonce,
		balance: balance,
	}
}

// NewBlock creates a new block
func (b Builder) NewBlock(parentID ids.ID, txs []*Tx) (*Block, error) {
	block := &Block{
		parentID: parentID,
		txs:      txs,
	}

	codec := Codec{}
	bytes, err := codec.MarshalBlock(block)
	if err != nil {
		return nil, err
	}

	block.bytes = bytes
	block.id = ids.NewID(hashing.ComputeHash256Array(block.bytes))
	return block, nil
}

// NewTx creates a new transaction from [key|nonce] for [amount] to [destination]
func (b Builder) NewTx(key *crypto.PrivateKeySECP256K1R, nonce, amount uint64, destination ids.ShortID) (*Tx, error) {
	if b.ChainID.IsZero() {
		return nil, errNilChainID
	}

	tx := &Tx{
		networkID:    b.NetworkID,
		chainID:      b.ChainID,
		nonce:        nonce,
		amount:       amount,
		to:           destination,
		verification: make(chan error, 1),
	}

	codec := Codec{}
	unsignedBytes, err := codec.MarshalUnsignedTx(tx)
	if err != nil {
		return nil, err
	}
	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		return nil, err
	}

	tx.sig = sig
	bytes, err := codec.MarshalTx(tx)
	if err != nil {
		return nil, err
	}

	tx.bytes = bytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return tx, nil
}
