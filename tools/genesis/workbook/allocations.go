// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package workbook

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/chain4travel/camino-node/tools/genesis/utils"
)

type AllocationColumn int

type AllocationRow struct {
	RowNo               int
	Bucket              string
	Kyc                 string
	Amount              uint64
	Address             ids.ShortID
	ConsortiumMember    string
	ControlGroup        string
	NodeID              ids.NodeID
	ValidatorPeriodDays uint32
	Additional1Percent  string
	OfferID             string
	FirstName           string
	TokenDeliveryOffset uint64
	DepositDuration     uint32
	PublicKey           string
}

func (a *AllocationRow) Header() []string { return []string{"#", "Company", "First Name"} }

func (a *AllocationRow) FromRow(fileRowNo int, row []string) error {
	// COLUMNS
	const (
		_RowNo AllocationColumn = iota
		_ComapnyName
		FirstName
		_LastName
		_KnownBy
		Kyc
		_Street
		_Street2
		_Zip
		_City
		_Country
		Bucket
		_CamPurchasePrice
		CamAmount
		PChainAddress
		PublicKey
		ConsortiumMember
		ControlGroup
		NodeID
		ValidationPeriodDays
		Additional1Percent
		OfferID
		AllocationStartOffet
		DepositDuration
	)

	var err error
	// xls file's rows are 1-indexed
	a.RowNo = fileRowNo + 1
	a.Bucket = row[Bucket]
	a.Kyc = strings.ToUpper(strings.TrimSpace(row[Kyc]))
	a.FirstName = row[FirstName]
	a.ConsortiumMember = strings.ToUpper(strings.TrimSpace(row[ConsortiumMember]))
	a.ControlGroup = strings.TrimSpace(row[ControlGroup])
	a.Additional1Percent = strings.TrimSpace(row[Additional1Percent])
	a.OfferID = strings.TrimSpace(row[OfferID])

	a.Amount, err = strconv.ParseUint(row[CamAmount], 10, 64)
	if err != nil {
		return fmt.Errorf("could not parse amount %s", row[CamAmount])
	}
	a.Amount *= units.Avax

	row[NodeID] = strings.TrimSpace(row[NodeID])
	if row[NodeID] != "" && row[NodeID] != "X" {
		a.NodeID, err = ids.NodeIDFromString(row[NodeID])
		if err != nil {
			return fmt.Errorf("could not parse node id: %s", row[NodeID])
		}
	}

	if row[ValidationPeriodDays] != "" {
		vpd, err := strconv.ParseUint(row[ValidationPeriodDays], 10, 32)
		a.ValidatorPeriodDays = uint32(vpd)
		if err != nil {
			return fmt.Errorf("could not parse Validator Period: %s", row[ValidationPeriodDays])
		}
	}

	keyRead := false
	if row[PublicKey] != "" {
		row[PublicKey] = strings.TrimPrefix(row[PublicKey], "0x")

		pk, err := utils.PublicKeyFromString(row[PublicKey])
		if err != nil {
			return fmt.Errorf("could not parse public key, expected uncompressed bytes %s", row[PublicKey])
		}
		addr, err := utils.ToPAddress(pk)
		if err != nil {
			return fmt.Errorf("[X/P] could not parse public key %s, %w", row[PublicKey], err)
		}

		a.Address, keyRead = addr, true
		a.PublicKey = row[PublicKey]
	}
	if !keyRead && len(row[PChainAddress]) >= 47 {
		_, _, addrBytes, err := address.Parse(strings.TrimSpace(row[PChainAddress]))
		if err != nil {
			return fmt.Errorf("could not parse address %s", row[PChainAddress])
		}
		a.Address, _ = ids.ToShortID(addrBytes)
	}

	a.TokenDeliveryOffset = 0
	if strings.TrimSpace(row[AllocationStartOffet]) != "" {
		a.TokenDeliveryOffset, err = strconv.ParseUint(row[AllocationStartOffet], 10, 64)
		if err != nil {
			return fmt.Errorf("could not parse allocation offset (%s) %w", row[AllocationStartOffet], err)
		}
	}

	dd, err := strconv.ParseUint(row[DepositDuration], 10, 32)
	if row[DepositDuration] != "" && err != nil {
		return fmt.Errorf("could not parse deposit duration %s: %w", row[DepositDuration], err)
	}
	a.DepositDuration = uint32(dd)

	if a.Kyc != YesValue && a.Kyc != NoValue {
		return fmt.Errorf("invalid KYC value (%s), can be either 'Y' or 'N'", a.Kyc)
	}

	if a.ConsortiumMember != "" && a.ConsortiumMember != CheckedValue {
		return fmt.Errorf("invalid consortium member value (%s), can be either empty or 'X'", a.ConsortiumMember)
	}

	if a.NodeID != ids.EmptyNodeID && a.ConsortiumMember != CheckedValue {
		return fmt.Errorf("non-consortium member validator node (with NodeID) defined")
	}

	return nil
}
