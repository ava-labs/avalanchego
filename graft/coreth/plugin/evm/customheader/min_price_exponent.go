// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
)

var errRemoteMinPriceExponentSet = errors.New("remote min price exponent should be nil")

// VerifyMinPriceExponent rejects any coreth block carrying a MinPriceExponent.
// The field belongs to SAE. semanticVerify only runs on coreth's own
// block.Verify, so coreth never verifies a post-SAE block and the field must
// always be absent.
func VerifyMinPriceExponent(header *types.Header) error {
	if customtypes.GetHeaderExtra(header).MinPriceExponent != nil {
		return fmt.Errorf("%w: %s", errRemoteMinPriceExponentSet, header.Hash())
	}
	return nil
}
