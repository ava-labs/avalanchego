// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/crypto"
)

var (
	errInvalidNil = errors.New("nil is invalid")
)

// Block is a group of transactions
type Block struct {
	id ids.ID

	parentID ids.ID
	txs      []*Tx

	bytes []byte
}

// ID of this operation
func (b *Block) ID() ids.ID { return b.id }

// ParentID of this operation
func (b *Block) ParentID() ids.ID { return b.parentID }

// Txs contained in the operation
func (b *Block) Txs() []*Tx { return b.txs }

// Bytes of this transaction
func (b *Block) Bytes() []byte { return b.bytes }

func (b *Block) startVerify(ctx *snow.Context, factory *crypto.FactorySECP256K1R) {
	if b != nil {
		for _, tx := range b.txs {
			tx.startVerify(ctx, factory)
		}
	}
}

func (b *Block) verify(ctx *snow.Context, factory *crypto.FactorySECP256K1R) error {
	switch {
	case b == nil:
		return errInvalidNil
	case b.id.IsZero():
		return errInvalidID
	case b.parentID.IsZero():
		return errInvalidID
	}

	b.startVerify(ctx, factory)

	for _, tx := range b.txs {
		if err := tx.verify(ctx, factory); err != nil {
			return err
		}
	}

	return nil
}
