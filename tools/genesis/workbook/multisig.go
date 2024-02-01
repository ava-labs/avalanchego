// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package workbook

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tools/genesis/utils"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
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

func (*MultiSigRow) Header() []string { return []string{"Control Group", "Threshold", "Company"} }

func (msig *MultiSigRow) FromRow(_ int, msigRow []string) error {
	// COLUMNS
	const (
		controlGroup MultiSigColumn = iota
		threshold
		_company
		_firstName
		_lastName
		_kyc
		pChainAddress
		publicKey
	)

	msig.ControlGroup = strings.TrimSpace(msigRow[controlGroup])
	if msigRow[threshold] != "" {
		thresholdUint64, err := strconv.ParseUint(msigRow[threshold], 10, 32)
		msig.Threshold = uint32(thresholdUint64)
		if err != nil {
			return fmt.Errorf("could not parse msig threshold %s", msigRow[threshold])
		}
	}

	keyRead := false
	var addr ids.ShortID
	if len(msigRow) > int(publicKey) && msigRow[publicKey] != "" {
		msigRow[publicKey] = strings.TrimPrefix(strings.TrimSpace(msigRow[publicKey]), "0x")

		pk, err := utils.PublicKeyFromString(msigRow[publicKey])
		if err != nil {
			return errors.New("could not parse public key")
		}
		addr, err = utils.ToPAddress(pk)
		if err != nil {
			return fmt.Errorf("[X/P] could not parse public key %s, %w", msigRow[publicKey], err)
		}
		keyRead = true
	}
	if !keyRead && len(msigRow[pChainAddress]) > 0 {
		_, _, addrBytes, err := address.Parse(strings.TrimSpace(msigRow[pChainAddress]))
		if err != nil {
			return fmt.Errorf("could not parse address %s for ctrl group %s - err: %s", msigRow[pChainAddress], msig.ControlGroup, err)
		}
		addr, _ = ids.ToShortID(addrBytes)
	}
	msig.Addr = addr

	return nil
}
