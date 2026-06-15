// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
)

var errRemoteMinPriceExponentSet = errors.New("remote min price exponent should be nil")

// VerifyMinPriceExponent rejects a header carrying a MinPriceExponent. coreth
// caps at Granite and never activates Helicon, so the field is always SAE's and
// must never appear on a coreth block.
func VerifyMinPriceExponent(
	config *extras.ChainConfig,
	header *types.Header,
) error {
	remote := customtypes.GetHeaderExtra(header).MinPriceExponent
	if !config.IsHelicon(header.Time) && remote != nil {
		return fmt.Errorf("%w: %s", errRemoteMinPriceExponentSet, header.Hash())
	}
	return nil
}
