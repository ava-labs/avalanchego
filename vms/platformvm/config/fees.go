// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

type TxFees struct {
	AddPrimaryNetworkValidator uint64 `json:"addPrimaryNetworkValidator"`
	AddPrimaryNetworkDelegator uint64 `json:"addPrimaryNetworkDelegator"`
	AddPOASubnetValidator      uint64 `json:"addPOASubnetValidator"`
	AddPOSSubnetValidator      uint64 `json:"addPOSSubnetValidator"`
	AddPOSSubnetDelegator      uint64 `json:"addPOSSubnetDelegator"`
	RemovePOASubnetValidator   uint64 `json:"removePOASubnetValidator"`
	CreateSubnet               uint64 `json:"createSubnet"`
	CreateChain                uint64 `json:"createChain"`
	TransformSubnet            uint64 `json:"transformSubnet"`
	Import                     uint64 `json:"import"`
	Export                     uint64 `json:"export"`
}

type TxFeeUpgrades struct {
	Initial       TxFees `json:"initial"`
	ApricotPhase3 TxFees `json:"apricotPhase3"`
	Blueberry     TxFees `json:"blueberry"`
}
