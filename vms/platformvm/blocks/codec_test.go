// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
)

func TestTypesNotOwner(t *testing.T) {
	outs := []any{
		(*ApricotProposalBlock)(nil),
		(*ApricotAbortBlock)(nil),
		(*ApricotCommitBlock)(nil),
		(*ApricotStandardBlock)(nil),
		(*ApricotAtomicBlock)(nil),

		(*BanffProposalBlock)(nil),
		(*BanffAbortBlock)(nil),
		(*BanffCommitBlock)(nil),
		(*BanffStandardBlock)(nil),
	}
	for _, out := range outs {
		t.Run(fmt.Sprintf("%T", out), func(t *testing.T) {
			_, ok := out.(fx.Owner)
			require.False(t, ok)
		})
	}
}
