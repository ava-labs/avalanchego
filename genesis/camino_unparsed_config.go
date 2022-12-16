// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/hex"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
)

var errCannotParseInitialAdmin = "cannot parse initialAdmin from genesis: %w"

type UnparsedCamino struct {
	VerifyNodeSignature      bool                       `json:"verifyNodeSignature"`
	LockModeBondDeposit      bool                       `json:"lockModeBondDeposit"`
	InitialAdmin             string                     `json:"initialAdmin"`
	DepositOffers            []genesis.DepositOffer     `json:"depositOffers"`
	Allocations              []UnparsedCaminoAllocation `json:"allocations"`
	InitialMultisigAddresses []UnparsedMultisigAlias    `json:"initialMultisigAddresses"`
}

func (uc UnparsedCamino) Parse() (Camino, error) {
	c := Camino{
		VerifyNodeSignature:      uc.VerifyNodeSignature,
		LockModeBondDeposit:      uc.LockModeBondDeposit,
		DepositOffers:            uc.DepositOffers,
		Allocations:              make([]CaminoAllocation, len(uc.Allocations)),
		InitialMultisigAddresses: make([]genesis.MultisigAlias, len(uc.InitialMultisigAddresses)),
	}

	_, _, avaxAddrBytes, err := address.Parse(uc.InitialAdmin)
	if err != nil {
		return c, fmt.Errorf(errCannotParseInitialAdmin, err)
	}
	avaxAddr, err := ids.ToShortID(avaxAddrBytes)
	if err != nil {
		return c, fmt.Errorf(errCannotParseInitialAdmin, err)
	}
	c.InitialAdmin = avaxAddr

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
	AddressState        uint64                       `json:"addressState"`
	PlatformAllocations []UnparsedPlatformAllocation `json:"platformAllocations"`
}

func (ua UnparsedCaminoAllocation) Parse() (CaminoAllocation, error) {
	a := CaminoAllocation{
		XAmount:             ua.XAmount,
		AddressState:        ua.AddressState,
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
	NodeID            string `json:"nodeID"`
	ValidatorDuration uint64 `json:"validatorDuration"`
	DepositOfferID    string `json:"depositOfferID"`
}

func (ua UnparsedPlatformAllocation) Parse() (PlatformAllocation, error) {
	a := PlatformAllocation{
		Amount:            ua.Amount,
		ValidatorDuration: ua.ValidatorDuration,
	}

	depositOfferID := ids.Empty
	if ua.DepositOfferID != "" {
		parsedDepositOfferID, err := ids.FromString(ua.DepositOfferID)
		if err != nil {
			return a, err
		}
		depositOfferID = parsedDepositOfferID
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
	a.DepositOfferID = depositOfferID

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
}

func (uma UnparsedMultisigAlias) Parse() (genesis.MultisigAlias, error) {
	ma := genesis.MultisigAlias{}

	var (
		err                   error
		aliasBytes, addrBytes []byte
		alias                 ids.ShortID
		addrs                 = make([]ids.ShortID, len(uma.Addresses))
	)

	if _, _, aliasBytes, err = address.Parse(uma.Alias); err == nil {
		alias, err = ids.ToShortID(aliasBytes)
	}
	if err != nil {
		return ma, err
	}

	for i, addr := range uma.Addresses {
		if _, _, addrBytes, err = address.Parse(addr); err == nil {
			addrs[i], err = ids.ToShortID(addrBytes)
		}
		if err != nil {
			return ma, err
		}
	}

	return genesis.MultisigAlias{
		Alias:     alias,
		Addresses: addrs,
		Threshold: uma.Threshold,
	}, nil
}
