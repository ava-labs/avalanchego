// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/hex"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
)

type Camino struct {
	VerifyNodeSignature      bool                    `json:"verifyNodeSignature"`
	LockModeBondDeposit      bool                    `json:"lockModeBondDeposit"`
	InitialAdmin             ids.ShortID             `json:"initialAdmin"`
	DepositOffers            []DepositOffer          `json:"depositOffers"`
	Allocations              []CaminoAllocation      `json:"allocations"`
	InitialMultisigAddresses []genesis.MultisigAlias `json:"initialMultisigAddresses"`
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
		"X",
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

	for i, ma := range c.InitialMultisigAddresses {
		err = uc.InitialMultisigAddresses[i].Unparse(ma, networkID)
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
		"X",
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

func (uma *UnparsedMultisigAlias) Unparse(msigAlias genesis.MultisigAlias, networkID uint32) error {
	addresses := make([]string, len(msigAlias.Addresses))
	for i, elem := range msigAlias.Addresses {
		addr, err := address.Format(configChainIDAlias, constants.GetHRP(networkID), elem.Bytes())
		if err != nil {
			return fmt.Errorf("while unparsing cannot format multisig address %s: %w", addr, err)
		}
		addresses[i] = addr
	}

	alias, err := address.Format(configChainIDAlias, constants.GetHRP(networkID), msigAlias.Alias.Bytes())
	if err != nil {
		return fmt.Errorf("while unparsing cannot format multisig alias %s: %w", msigAlias.Alias, err)
	}

	uma.Alias = alias
	uma.Addresses = addresses
	uma.Threshold = msigAlias.Threshold
	uma.Memo = msigAlias.Memo

	return nil
}

type AddressStates struct {
	ConsortiumMember bool `json:"consortiumMember"`
	KYCVerified      bool `json:"kycVerified"`
}

type DepositOffer struct {
	InterestRateNominator   uint64 `json:"interestRateNominator"`
	Start                   uint64 `json:"start"`
	End                     uint64 `json:"end"`
	MinAmount               uint64 `json:"minAmount"`
	MinDuration             uint32 `json:"minDuration"`
	MaxDuration             uint32 `json:"maxDuration"`
	UnlockPeriodDuration    uint32 `json:"unlockPeriodDuration"`
	NoRewardsPeriodDuration uint32 `json:"noRewardsPeriodDuration"`
	Memo                    string `json:"memo"`
	Flags                   uint64 `json:"flags"`
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
