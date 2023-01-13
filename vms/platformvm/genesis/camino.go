// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
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
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Camino genesis args
type Camino struct {
	VerifyNodeSignature      bool                     `serialize:"true"`
	LockModeBondDeposit      bool                     `serialize:"true"`
	InitialAdmin             ids.ShortID              `serialize:"true"`
	AddressStates            []AddressState           `serialize:"true"`
	DepositOffers            []DepositOffer           `serialize:"true"`
	Blocks                   []*Block                 `serialize:"true"` // arranged in a block order
	ConsortiumMembersNodeIDs []ConsortiumMemberNodeID `serialize:"true"`
	InitialMultisigAddresses []MultisigAlias          `serialize:"true"`
}

func (c *Camino) Init() error {
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

type DepositOffer struct {
	OfferID                 ids.ID `serialize:"false"`
	InterestRateNominator   uint64 `serialize:"true" json:"interestRateNominator"`
	Start                   uint64 `serialize:"true" json:"start"`
	End                     uint64 `serialize:"true" json:"end"`
	MinAmount               uint64 `serialize:"true" json:"minAmount"`
	MinDuration             uint32 `serialize:"true" json:"minDuration"`
	MaxDuration             uint32 `serialize:"true" json:"maxDuration"`
	UnlockPeriodDuration    uint32 `serialize:"true" json:"unlockPeriodDuration"`
	NoRewardsPeriodDuration uint32 `serialize:"true" json:"noRewardsPeriodDuration"`
	Flags                   uint64 `serialize:"true" json:"flags"`
}

// Gets offer id from its bytes hash
func (offer DepositOffer) ID() (ids.ID, error) {
	bytes, err := blocks.GenesisCodec.Marshal(blocks.Version, offer)
	if err != nil {
		return ids.Empty, err
	}
	return hashing.ComputeHash256Array(bytes), nil
}

func (offer DepositOffer) Verify() error {
	if offer.Start >= offer.End {
		return fmt.Errorf(
			"deposit offer starttime (%v) is not before its endtime (%v)",
			offer.Start,
			offer.End,
		)
	}

	if offer.MinDuration > offer.MaxDuration {
		return errors.New("deposit minimum duration is greater than maximum duration")
	}

	if offer.MinDuration == 0 {
		return errors.New("deposit offer has zero minimum duration")
	}

	if offer.MinDuration < offer.NoRewardsPeriodDuration {
		return fmt.Errorf(
			"deposit offer minimum duration (%v) is less than no-rewards period duration (%v)",
			offer.MinDuration,
			offer.NoRewardsPeriodDuration,
		)
	}

	if offer.MinDuration < offer.UnlockPeriodDuration {
		return fmt.Errorf(
			"deposit offer minimum duration (%v) is less than unlock period duration (%v)",
			offer.MinDuration,
			offer.UnlockPeriodDuration,
		)
	}

	calcID, err := offer.ID()
	if err != nil {
		return err
	}
	if offer.OfferID != calcID {
		return fmt.Errorf("deposit offer ID (%s) mismatched with the calculated one (%s)", offer.OfferID, calcID)
	}

	return nil
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
