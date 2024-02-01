// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package workbook

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/chain4travel/camino-node/tools/genesis/utils"
)

type MultiSigGroup struct {
	ControlGroup string
	Threshold    uint32
	Addrs        []ids.ShortID
}

type MultiSigColumn int

type MultiSigRow struct {
	ControlGroup string
	Threshold    uint32
	Addr         ids.ShortID
}

func (msig *MultiSigRow) Header() []string { return []string{"Control Group", "Threshold", "Company"} }

func (msig *MultiSigRow) FromRow(_ int, msigRow []string) error {
	// COLUMNS
	const (
		ControlGroup MultiSigColumn = iota
		Threshold
		_Company
		_FirstName
		_LastName
		_Kyc
		PChainAddress
		PublicKey
	)

	msig.ControlGroup = strings.TrimSpace(msigRow[ControlGroup])
	if msigRow[Threshold] != "" {
		threshold, err := strconv.ParseUint(msigRow[Threshold], 10, 32)
		msig.Threshold = uint32(threshold)
		if err != nil {
			return fmt.Errorf("could not parse msig threshold %s", msigRow[Threshold])
		}
	}

	keyRead := false
	var addr ids.ShortID
	if len(msigRow) > int(PublicKey) && msigRow[PublicKey] != "" {
		msigRow[PublicKey] = strings.TrimPrefix(strings.TrimSpace(msigRow[PublicKey]), "0x")

		pk, err := utils.PublicKeyFromString(msigRow[PublicKey])
		if err != nil {
			return fmt.Errorf("could not parse public key")
		}
		addr, err = utils.ToPAddress(pk)
		if err != nil {
			return fmt.Errorf("[X/P] could not parse public key %s, %w", msigRow[PublicKey], err)
		}
		keyRead = true
	}
	if !keyRead && len(msigRow[PChainAddress]) > 0 {
		_, _, addrBytes, err := address.Parse(strings.TrimSpace(msigRow[PChainAddress]))
		if err != nil {
			return fmt.Errorf("could not parse address %s for ctrl group %s - err: %s", msigRow[PChainAddress], msig.ControlGroup, err)
		}
		addr, _ = ids.ToShortID(addrBytes)
	}
	msig.Addr = addr

	return nil
}
