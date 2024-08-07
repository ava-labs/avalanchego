// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
)

type Camino struct {
	VerifyNodeSignature      bool               `json:"verifyNodeSignature"`
	LockModeBondDeposit      bool               `json:"lockModeBondDeposit"`
	InitialAdmin             ids.ShortID        `json:"initialAdmin"`
	DepositOffers            []DepositOffer     `json:"depositOffers"`
	Allocations              []CaminoAllocation `json:"allocations"`
	InitialMultisigAddresses []MultisigAlias    `json:"initialMultisigAddresses"`
}

func (c Camino) Unparse(networkID uint32, starttime uint64) (UnparsedCamino, error) {
	uc := UnparsedCamino{
		VerifyNodeSignature:      c.VerifyNodeSignature,
		LockModeBondDeposit:      c.LockModeBondDeposit,
		DepositOffers:            make([]UnparsedDepositOffer, len(c.DepositOffers)),
		Allocations:              make([]UnparsedCaminoAllocation, len(c.Allocations)),
		InitialMultisigAddresses: make([]UnparsedMultisigAlias, len(c.InitialMultisigAddresses)),
	}

	avaxAddr, err := address.Format(
		configChainIDAlias,
		constants.GetHRP(networkID),
		c.InitialAdmin.Bytes(),
	)
	if err != nil {
		return uc, err
	}
	uc.InitialAdmin = avaxAddr

	for i, a := range c.Allocations {
		ua, err := a.Unparse(networkID)
		if err != nil {
			return uc, err
		}
		uc.Allocations[i] = ua
	}

	for i := range uc.DepositOffers {
		uc.DepositOffers[i], err = c.DepositOffers[i].Unparse(starttime)
		if err != nil {
			return uc, err
		}
	}

	for i := range c.InitialMultisigAddresses {
		uc.InitialMultisigAddresses[i], err = c.InitialMultisigAddresses[i].Unparse(networkID)
		if err != nil {
			return uc, err
		}
	}

	return uc, nil
}

func (c Camino) InitialSupply() (uint64, error) {
	initialSupply := uint64(0)
	for _, allocation := range c.Allocations {
		newInitialSupply, err := math.Add64(initialSupply, allocation.XAmount)
		if err != nil {
			return 0, err
		}
		for _, platformAllocation := range allocation.PlatformAllocations {
			newInitialSupply, err = math.Add64(newInitialSupply, platformAllocation.Amount)
			if err != nil {
				return 0, err
			}
		}
		initialSupply = newInitialSupply
	}
	return initialSupply, nil
}

type CaminoAllocation struct {
	ETHAddr             ids.ShortID          `json:"ethAddr"`
	AVAXAddr            ids.ShortID          `json:"avaxAddr"`
	XAmount             uint64               `json:"xAmount"`
	AddressStates       AddressStates        `json:"addressStates"`
	PlatformAllocations []PlatformAllocation `json:"platformAllocations"`
}

func (a CaminoAllocation) Unparse(networkID uint32) (UnparsedCaminoAllocation, error) {
	ua := UnparsedCaminoAllocation{
		XAmount:             a.XAmount,
		ETHAddr:             "0x" + hex.EncodeToString(a.ETHAddr.Bytes()),
		AddressStates:       a.AddressStates,
		PlatformAllocations: make([]UnparsedPlatformAllocation, len(a.PlatformAllocations)),
	}
	avaxAddr, err := address.Format(
		configChainIDAlias,
		constants.GetHRP(networkID),
		a.AVAXAddr.Bytes(),
	)
	ua.AVAXAddr = avaxAddr

	for i, pa := range a.PlatformAllocations {
		upa, err := pa.Unparse()
		if err != nil {
			return ua, err
		}
		ua.PlatformAllocations[i] = upa
	}

	return ua, err
}

func (a CaminoAllocation) Less(other CaminoAllocation) bool {
	return a.XAmount < other.XAmount ||
		(a.XAmount == other.XAmount && a.AVAXAddr.Less(other.AVAXAddr))
}

type PlatformAllocation struct {
	Amount            uint64     `json:"amount"`
	NodeID            ids.NodeID `json:"nodeID"`
	ValidatorDuration uint64     `json:"validatorDuration"`
	DepositDuration   uint64     `json:"depositDuration"`
	TimestampOffset   uint64     `json:"timestampOffset"`
	DepositOfferMemo  string     `json:"depositOfferMemo"`
	Memo              string     `json:"memo"`
}

func (a PlatformAllocation) Unparse() (UnparsedPlatformAllocation, error) {
	ua := UnparsedPlatformAllocation{
		Amount:            a.Amount,
		ValidatorDuration: a.ValidatorDuration,
		DepositDuration:   a.DepositDuration,
		DepositOfferMemo:  a.DepositOfferMemo,
		TimestampOffset:   a.TimestampOffset,
		Memo:              a.Memo,
	}

	if a.NodeID != ids.EmptyNodeID {
		ua.NodeID = a.NodeID.String()
	}

	return ua, nil
}

type MultisigAlias struct {
	Alias     ids.ShortID   `serialize:"true" json:"alias"`
	Threshold uint32        `serialize:"true" json:"threshold"`
	Addresses []ids.ShortID `serialize:"true" json:"addresses"`
	Memo      string        `serialize:"true" json:"memo"`
}

func (ma MultisigAlias) Unparse(networkID uint32) (UnparsedMultisigAlias, error) {
	uma := UnparsedMultisigAlias{
		Threshold: ma.Threshold,
		Addresses: make([]string, len(ma.Addresses)),
		Memo:      ma.Memo,
	}

	for i, elem := range ma.Addresses {
		addr, err := address.Format(configChainIDAlias, constants.GetHRP(networkID), elem.Bytes())
		if err != nil {
			return uma, fmt.Errorf("while unparsing cannot format multisig address %s: %w", addr, err)
		}
		uma.Addresses[i] = addr
	}

	alias, err := address.Format(configChainIDAlias, constants.GetHRP(networkID), ma.Alias.Bytes())
	if err != nil {
		return uma, fmt.Errorf("while unparsing cannot format multisig alias %s: %w", ma.Alias, err)
	}

	uma.Alias = alias

	return uma, nil
}

func (ma MultisigAlias) ComputeAlias(txID ids.ID) ids.ShortID {
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

	return multisig.ComputeAliasID(hashing.ComputeHash256Array(allBytes))
}

type AddressStates struct {
	ConsortiumMember bool `json:"consortiumMember"`
	KYCVerified      bool `json:"kycVerified"`
}

type DepositOffer struct {
	InterestRateNominator   uint64            `json:"interestRateNominator"`
	Start                   uint64            `json:"start"`
	End                     uint64            `json:"end"`
	MinAmount               uint64            `json:"minAmount"`
	MinDuration             uint32            `json:"minDuration"`
	MaxDuration             uint32            `json:"maxDuration"`
	UnlockPeriodDuration    uint32            `json:"unlockPeriodDuration"`
	NoRewardsPeriodDuration uint32            `json:"noRewardsPeriodDuration"`
	Memo                    string            `json:"memo"`
	Flags                   deposit.OfferFlag `json:"flags"`
}

func (parsedOffer DepositOffer) Unparse(startime uint64) (UnparsedDepositOffer, error) {
	unparsedOffer := UnparsedDepositOffer{
		InterestRateNominator:   parsedOffer.InterestRateNominator,
		MinAmount:               parsedOffer.MinAmount,
		MinDuration:             parsedOffer.MinDuration,
		MaxDuration:             parsedOffer.MaxDuration,
		UnlockPeriodDuration:    parsedOffer.UnlockPeriodDuration,
		NoRewardsPeriodDuration: parsedOffer.NoRewardsPeriodDuration,
		Memo:                    parsedOffer.Memo,
	}

	offerStartOffset, err := math.Sub(parsedOffer.Start, startime)
	if err != nil {
		return unparsedOffer, err
	}
	unparsedOffer.StartOffset = offerStartOffset

	offerEndOffset, err := math.Sub(parsedOffer.End, startime)
	if err != nil {
		return unparsedOffer, err
	}
	unparsedOffer.EndOffset = offerEndOffset

	if parsedOffer.Flags&deposit.OfferFlagLocked != 0 {
		unparsedOffer.Flags.Locked = true
	}

	return unparsedOffer, nil
}
