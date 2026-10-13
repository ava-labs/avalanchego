// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
)

var errRemoteTargetExponentSet = errors.New("remote target exponent should be nil")

// VerifyTargetExponent rejects any coreth block carrying a TargetExponent.
// The ACP-176 field belongs to SAE. semanticVerify only runs on coreth's own
// block.Verify, so coreth never verifies a post-SAE block and the field must
// always be absent.
func VerifyTargetExponent(header *types.Header) error {
	if customtypes.GetHeaderExtra(header).TargetExponent != nil {
		return fmt.Errorf("%w: %s", errRemoteTargetExponentSet, header.Hash())
	}
	return nil
}
