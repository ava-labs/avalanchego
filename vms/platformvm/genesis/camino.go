// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Camino genesis args
type Camino struct {
	VerifyNodeSignature      bool                     `serialize:"true"`
	LockModeBondDeposit      bool                     `serialize:"true"`
	InitialAdmin             ids.ShortID              `serialize:"true"`
	AddressStates            []AddressState           `serialize:"true"`
	DepositOffers            []*deposit.Offer         `serialize:"true"`
	Blocks                   []*Block                 `serialize:"true"` // arranged in a block order
	ConsortiumMembersNodeIDs []ConsortiumMemberNodeID `serialize:"true"`
	MultisigAliases          []*multisig.Alias        `serialize:"true"`
}

func (c *Camino) Init() error {
	for _, offer := range c.DepositOffers {
		if err := SetDepositOfferID(offer); err != nil {
			return err
		}
	}
	for _, block := range c.Blocks {
		if err := block.Init(); err != nil {
			return err
		}
	}
	return nil
}

type ConsortiumMemberNodeID struct {
	ConsortiumMemberAddress ids.ShortID `serialize:"true"`
	NodeID                  ids.NodeID  `serialize:"true"`
}

type AddressState struct {
	Address ids.ShortID     `serialize:"true"`
	State   as.AddressState `serialize:"true"`
}

type Block struct {
	Timestamp        uint64    `serialize:"true"`
	Deposits         []*txs.Tx `serialize:"true"`
	Validators       []*txs.Tx `serialize:"true"`
	UnlockedUTXOsTxs []*txs.Tx `serialize:"true"`
}

func (b *Block) Init() error {
	for _, tx := range b.Validators {
		if err := tx.Initialize(txs.GenesisCodec); err != nil {
			return err
		}
	}
	for _, tx := range b.Deposits {
		if err := tx.Initialize(txs.GenesisCodec); err != nil {
			return err
		}
	}
	for _, tx := range b.UnlockedUTXOsTxs {
		if err := tx.Initialize(txs.GenesisCodec); err != nil {
			return err
		}
	}
	return nil
}

func (b *Block) Less(b1 *Block) bool {
	return b.Timestamp < b1.Timestamp
}

func (b *Block) Time() time.Time {
	return time.Unix(int64(b.Timestamp), 0)
}

func (b *Block) Txs() []*txs.Tx {
	txs := make([]*txs.Tx, 0, len(b.Validators)+len(b.Deposits)+len(b.UnlockedUTXOsTxs))
	txs = append(txs, b.Validators...)
	txs = append(txs, b.Deposits...)
	return append(txs, b.UnlockedUTXOsTxs...)
}

// Generate deposit offer id from its bytes hash and set it to offer's ID field
func SetDepositOfferID(offer *deposit.Offer) error {
	bytes, err := txs.GenesisCodec.Marshal(txs.Version, offer)
	if err != nil {
		return err
	}
	offer.ID = hashing.ComputeHash256Array(bytes)
	return nil
}
