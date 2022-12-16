// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

// Camino genesis args
type Camino struct {
	VerifyNodeSignature      bool            `serialize:"true"`
	LockModeBondDeposit      bool            `serialize:"true"`
	InitialAdmin             ids.ShortID     `serialize:"true"`
	AddressStates            []AddressState  `serialize:"true"`
	DepositOffers            []DepositOffer  `serialize:"true"`
	Deposits                 []*txs.Tx       `serialize:"true"`
	InitialMultisigAddresses []MultisigAlias `serialize:"true" json:"initialMultisigAddresses"`
}

type AddressState struct {
	Address ids.ShortID `serialize:"true" json:"address"`
	State   uint64      `serialize:"true" json:"state"`
}

type DepositOffer struct {
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

	return nil
}

type MultisigAlias struct {
	Alias     ids.ShortID   `serialize:"true" json:"alias"`
	Threshold uint32        `serialize:"true" json:"threshold"`
	Addresses []ids.ShortID `serialize:"true" json:"addresses"`
}

func NewMultisigAlias(txID ids.ID, addresses []ids.ShortID, threshold uint32) (MultisigAlias, error) {
	var err error

	// make sure provided addresses are unique and sorted
	addrSet := set.NewSet[ids.ShortID](len(addresses))
	addrSet.Add(addresses...)
	sortedAddrs := addrSet.List()
	utils.Sort(sortedAddrs)

	if len(sortedAddrs) < int(threshold) {
		return MultisigAlias{}, fmt.Errorf("threshold %d is greater than the number of addresses %d", threshold, len(sortedAddrs))
	}

	ma := MultisigAlias{
		Addresses: sortedAddrs,
		Threshold: threshold,
	}
	ma.Alias = ma.ComputeAlias(txID)

	return ma, err
}

func (ma *MultisigAlias) ComputeAlias(txID ids.ID) ids.ShortID {
	txIDBytes := txID[:]
	thresholdBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(thresholdBytes, ma.Threshold)
	allBytes := make([]byte, 32+4+20*len(ma.Addresses))

	copy(allBytes, txIDBytes)
	copy(allBytes[32:], thresholdBytes)

	beg := 32 + 4
	for _, addr := range ma.Addresses {
		copy(allBytes[beg:], addr.Bytes())
		beg += 20
	}

	alias, _ := ids.ToShortID(hashing.ComputeHash160(hashing.ComputeHash256(allBytes)))
	return alias
}

func (ma *MultisigAlias) Verify(txID ids.ID) error {
	// double check that the addresses are unique
	addrSet := set.NewSet[ids.ShortID](len(ma.Addresses))
	addrSet.Add(ma.Addresses...)
	if addrSet.Len() != len(ma.Addresses) {
		return errors.New("duplicate addresses found in multisig alias")
	}

	// double check passed addresses are sorted, as required
	if !utils.IsSortedAndUniqueSortable(ma.Addresses) {
		return fmt.Errorf("addresses must be sorted and unique")
	}

	alias := ma.ComputeAlias(txID)
	if alias != ma.Alias {
		return fmt.Errorf("calculated alias does not match provided one")
	}

	return nil
}
