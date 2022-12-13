// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"encoding/hex"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
)

type Camino struct {
	VerifyNodeSignature bool                   `json:"verifyNodeSignature"`
	LockModeBondDeposit bool                   `json:"lockModeBondDeposit"`
	InitialAdmin        ids.ShortID            `json:"initialAdmin"`
	DepositOffers       []genesis.DepositOffer `json:"depositOffers"`
	Allocations         []CaminoAllocation     `json:"allocations"`
}

func (c Camino) Unparse(networkID uint32) (UnparsedCamino, error) {
	uc := UnparsedCamino{
		VerifyNodeSignature: c.VerifyNodeSignature,
		LockModeBondDeposit: c.LockModeBondDeposit,
		DepositOffers:       c.DepositOffers,
		Allocations:         make([]UnparsedCaminoAllocation, len(c.Allocations)),
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
	PlatformAllocations []PlatformAllocation `json:"platformAllocations"`
}

func (a CaminoAllocation) Unparse(networkID uint32) (UnparsedCaminoAllocation, error) {
	ua := UnparsedCaminoAllocation{
		XAmount:             a.XAmount,
		ETHAddr:             "0x" + hex.EncodeToString(a.ETHAddr.Bytes()),
		PlatformAllocations: a.PlatformAllocations,
	}
	avaxAddr, err := address.Format(
		"X",
		constants.GetHRP(networkID),
		a.AVAXAddr.Bytes(),
	)
	ua.AVAXAddr = avaxAddr

	return ua, err
}

type PlatformAllocation struct {
	Amount         uint64     `json:"amount"`
	NodeID         ids.NodeID `json:"nodeID"`
	DepositOfferID ids.ID     `json:"depositOfferID"`
}
