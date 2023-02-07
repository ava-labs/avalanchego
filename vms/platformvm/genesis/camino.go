// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
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
	InitialMultisigAddresses []MultisigAlias          `serialize:"true"`
}

func (c *Camino) Init() error {
	for _, offer := range c.DepositOffers {
		if err := offer.SetID(); err != nil {
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
	Address ids.ShortID `serialize:"true"`
	State   uint64      `serialize:"true"`
}

type MultisigAlias struct {
	Alias     ids.ShortID   `serialize:"true" json:"alias"`
	Threshold uint32        `serialize:"true" json:"threshold"`
	Addresses []ids.ShortID `serialize:"true" json:"addresses"`
	Memo      string        `serialize:"true" json:"memo"`
}

func NewMultisigAlias(txID ids.ID, addresses []ids.ShortID, threshold uint32, memo string) (MultisigAlias, error) {
	// make sure provided addresses are unique and sorted
	addrSet := set.NewSet[ids.ShortID](len(addresses))
	addrSet.Add(addresses...)
	sortedAddrs := addrSet.List()
	utils.Sort(sortedAddrs)

	ma := MultisigAlias{
		Addresses: sortedAddrs,
		Threshold: threshold,
		Memo:      memo,
	}
	ma.Alias = ma.ComputeAlias(txID)

	return ma, ma.Verify(txID)
}

func (ma *MultisigAlias) ComputeAlias(txID ids.ID) ids.ShortID {
	txIDBytes := txID[:]
	thresholdBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(thresholdBytes, ma.Threshold)
	memoLen := len(ma.Memo)
	allBytes := make([]byte, 32+4+memoLen+20*len(ma.Addresses))

	copy(allBytes, txIDBytes)
	copy(allBytes[32:], thresholdBytes)
	copy(allBytes[32+4:], ma.Memo)

	beg := 32 + 4 + memoLen
	for _, addr := range ma.Addresses {
		copy(allBytes[beg:], addr.Bytes())
		beg += 20
	}

	alias, _ := ids.ToShortID(hashing.ComputeHash160(hashing.ComputeHash256(allBytes)))
	return alias
}

func (ma *MultisigAlias) Verify(txID ids.ID) error {
	if len(ma.Memo) > avax.MaxMemoSize {
		return fmt.Errorf("msig alias memo is larger (%d bytes) than max of %d bytes", len(ma.Memo), avax.MaxMemoSize)
	}

	// double check that the addresses are unique
	addrSet := set.NewSet[ids.ShortID](len(ma.Addresses))
	addrSet.Add(ma.Addresses...)
	if addrSet.Len() != len(ma.Addresses) {
		return errors.New("duplicate addresses found in multisig alias")
	}

	if ma.Threshold == 0 || addrSet.Len() < int(ma.Threshold) {
		return errors.New("msig alias threshold is greater, than the number of addresses")
	}

	// double check passed addresses are sorted, as required
	if !utils.IsSortedAndUniqueSortable(ma.Addresses) {
		return fmt.Errorf("addresses must be sorted and unique")
	}

	alias := ma.ComputeAlias(txID)
	if alias != ma.Alias {
		calcAlias, _ := address.Format("X", constants.CaminoHRP, alias.Bytes())
		providedAlias, _ := address.Format("X", constants.CaminoHRP, ma.Alias.Bytes())
		return fmt.Errorf("provided alias (%s) does not match calculated one (%s)", providedAlias, calcAlias)
	}

	return nil
}

type Block struct {
	Timestamp        uint64    `serialize:"true"`
	Deposits         []*txs.Tx `serialize:"true"`
	Validators       []*txs.Tx `serialize:"true"`
	UnlockedUTXOsTxs []*txs.Tx `serialize:"true"`
}

func (b *Block) Init() error {
	for _, tx := range b.Validators {
		if err := tx.Sign(txs.GenesisCodec, nil); err != nil {
			return err
		}
	}
	for _, tx := range b.Deposits {
		if err := tx.Sign(txs.GenesisCodec, nil); err != nil {
			return err
		}
	}
	for _, tx := range b.UnlockedUTXOsTxs {
		if err := tx.Sign(txs.GenesisCodec, nil); err != nil {
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
