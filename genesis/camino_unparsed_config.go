// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
)

var errCannotParseInitialAdmin = errors.New("cannot parse initialAdmin from genesis")

type UnparsedCamino struct {
	VerifyNodeSignature      bool                       `json:"verifyNodeSignature"`
	LockModeBondDeposit      bool                       `json:"lockModeBondDeposit"`
	InitialAdmin             string                     `json:"initialAdmin"`
	DepositOffers            []UnparsedDepositOffer     `json:"depositOffers"`
	Allocations              []UnparsedCaminoAllocation `json:"allocations"`
	InitialMultisigAddresses []UnparsedMultisigAlias    `json:"initialMultisigAddresses"`
}

func (uc UnparsedCamino) Parse(startTime uint64) (Camino, error) {
	c := Camino{
		VerifyNodeSignature:      uc.VerifyNodeSignature,
		LockModeBondDeposit:      uc.LockModeBondDeposit,
		DepositOffers:            make([]DepositOffer, len(uc.DepositOffers)),
		Allocations:              make([]CaminoAllocation, len(uc.Allocations)),
		InitialMultisigAddresses: make([]MultisigAlias, len(uc.InitialMultisigAddresses)),
	}

	_, _, avaxAddrBytes, err := address.Parse(uc.InitialAdmin)
	if err != nil {
		return c, fmt.Errorf("%w: %v", errCannotParseInitialAdmin, err)
	}
	avaxAddr, err := ids.ToShortID(avaxAddrBytes)
	if err != nil {
		return c, fmt.Errorf("%w: %v", errCannotParseInitialAdmin, err)
	}
	c.InitialAdmin = avaxAddr

	for i, udo := range uc.DepositOffers {
		offer, err := udo.Parse(startTime)
		if err != nil {
			return c, err
		}
		c.DepositOffers[i] = offer
	}

	for i, ua := range uc.Allocations {
		a, err := ua.Parse()
		if err != nil {
			return c, err
		}
		c.Allocations[i] = a
	}

	for i, uma := range uc.InitialMultisigAddresses {
		c.InitialMultisigAddresses[i], err = uma.Parse()
		if err != nil {
			return c, err
		}
	}

	return c, nil
}

type UnparsedCaminoAllocation struct {
	ETHAddr             string                       `json:"ethAddr"`
	AVAXAddr            string                       `json:"avaxAddr"`
	XAmount             uint64                       `json:"xAmount"`
	AddressStates       AddressStates                `json:"addressStates"`
	PlatformAllocations []UnparsedPlatformAllocation `json:"platformAllocations"`
}

func (ua UnparsedCaminoAllocation) Parse() (CaminoAllocation, error) {
	a := CaminoAllocation{
		XAmount:             ua.XAmount,
		AddressStates:       ua.AddressStates,
		PlatformAllocations: make([]PlatformAllocation, len(ua.PlatformAllocations)),
	}

	if len(ua.ETHAddr) < 2 {
		return a, errInvalidETHAddress
	}

	ethAddrBytes, err := hex.DecodeString(ua.ETHAddr[2:])
	if err != nil {
		return a, err
	}
	ethAddr, err := ids.ToShortID(ethAddrBytes)
	if err != nil {
		return a, err
	}
	a.ETHAddr = ethAddr

	_, _, avaxAddrBytes, err := address.Parse(ua.AVAXAddr)
	if err != nil {
		return a, err
	}
	avaxAddr, err := ids.ToShortID(avaxAddrBytes)
	if err != nil {
		return a, err
	}
	a.AVAXAddr = avaxAddr

	for i, upa := range ua.PlatformAllocations {
		pa, err := upa.Parse()
		if err != nil {
			return a, err
		}
		a.PlatformAllocations[i] = pa
	}

	return a, nil
}

type UnparsedPlatformAllocation struct {
	Amount            uint64 `json:"amount"`
	NodeID            string `json:"nodeID,omitempty"`
	ValidatorDuration uint64 `json:"validatorDuration,omitempty"`
	DepositDuration   uint64 `json:"depositDuration,omitempty"`
	TimestampOffset   uint64 `json:"timestampOffset,omitempty"`
	DepositOfferMemo  string `json:"depositOfferMemo,omitempty"`
	Memo              string `json:"memo,omitempty"`
}

func (ua UnparsedPlatformAllocation) Parse() (PlatformAllocation, error) {
	a := PlatformAllocation{
		Amount:            ua.Amount,
		ValidatorDuration: ua.ValidatorDuration,
		DepositDuration:   ua.DepositDuration,
		DepositOfferMemo:  ua.DepositOfferMemo,
		TimestampOffset:   ua.TimestampOffset,
		Memo:              ua.Memo,
	}

	nodeID := ids.EmptyNodeID
	if ua.NodeID != "" {
		parsedNodeID, err := ids.NodeIDFromString(ua.NodeID)
		if err != nil {
			return a, err
		}
		nodeID = parsedNodeID
	}

	a.NodeID = nodeID

	return a, nil
}

// UnparsedMultisigAlias defines a multisignature alias address.
// [Alias] is the alias of the multisignature address. It's encoded to string
// the same way as ShortID String() method does.
// [Addresses] are the addresses that are allowed to sign transactions from the multisignature address.
// All addresses are encoded to string the same way as ShortID String() method does.
// [Threshold] is the number of signatures required to sign transactions from the multisignature address.
type UnparsedMultisigAlias struct {
	Alias     string   `json:"alias"`
	Addresses []string `json:"addresses"`
	Threshold uint32   `json:"threshold"`
	Memo      string   `json:"memo,omitempty"`
}

func (uma UnparsedMultisigAlias) Parse() (MultisigAlias, error) {
	ma := MultisigAlias{
		Threshold: uma.Threshold,
		Memo:      uma.Memo,
		Addresses: make([]ids.ShortID, len(uma.Addresses)),
	}

	alias, err := address.ParseToID(uma.Alias)
	if err != nil {
		return ma, err
	}
	ma.Alias = alias

	for i, unparsedAddr := range uma.Addresses {
		addr, err := address.ParseToID(unparsedAddr)
		if err != nil {
			return ma, err
		}
		ma.Addresses[i] = addr
	}

	return ma, nil
}

type UnparsedDepositOffer struct {
	InterestRateNominator   uint64                    `json:"interestRateNominator"`
	StartOffset             uint64                    `json:"startOffset"`
	EndOffset               uint64                    `json:"endOffset"`
	MinAmount               uint64                    `json:"minAmount"`
	MinDuration             uint32                    `json:"minDuration"`
	MaxDuration             uint32                    `json:"maxDuration"`
	UnlockPeriodDuration    uint32                    `json:"unlockPeriodDuration"`
	NoRewardsPeriodDuration uint32                    `json:"noRewardsPeriodDuration"`
	Memo                    string                    `json:"memo"`
	Flags                   UnparsedDepositOfferFlags `json:"flags"`
}

type UnparsedDepositOfferFlags struct {
	Locked bool `json:"locked"`
}

func (udo UnparsedDepositOffer) Parse(startTime uint64) (DepositOffer, error) {
	do := DepositOffer{
		InterestRateNominator:   udo.InterestRateNominator,
		MinAmount:               udo.MinAmount,
		MinDuration:             udo.MinDuration,
		MaxDuration:             udo.MaxDuration,
		UnlockPeriodDuration:    udo.UnlockPeriodDuration,
		NoRewardsPeriodDuration: udo.NoRewardsPeriodDuration,
		Memo:                    udo.Memo,
	}

	offerStartTime, err := math.Add64(startTime, udo.StartOffset)
	if err != nil {
		return do, err
	}
	do.Start = offerStartTime

	offerEndTime, err := math.Add64(startTime, udo.EndOffset)
	if err != nil {
		return do, err
	}
	do.End = offerEndTime

	if udo.Flags.Locked {
		do.Flags |= deposit.OfferFlagLocked
	}

	return do, nil
}
