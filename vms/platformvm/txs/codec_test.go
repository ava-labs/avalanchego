// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestTypesNotOwner(t *testing.T) {
	outs := []any{
		(*secp256k1fx.TransferInput)(nil),
		(*secp256k1fx.TransferOutput)(nil),
		(*secp256k1fx.Credential)(nil),
		(*secp256k1fx.Input)(nil),

		(*AddValidatorTx)(nil),
		(*AddSubnetValidatorTx)(nil),
		(*AddDelegatorTx)(nil),
		(*CreateChainTx)(nil),
		(*CreateSubnetTx)(nil),
		(*ImportTx)(nil),
		(*ExportTx)(nil),
		(*AdvanceTimeTx)(nil),
		(*RewardValidatorTx)(nil),

		(*stakeable.LockIn)(nil),
		(*stakeable.LockOut)(nil),

		(*RemoveSubnetValidatorTx)(nil),
		(*TransformSubnetTx)(nil),
		(*AddPermissionlessValidatorTx)(nil),
		(*AddPermissionlessDelegatorTx)(nil),

		(*signer.Empty)(nil),
		(*signer.ProofOfPossession)(nil),
	}
	for _, out := range outs {
		t.Run(fmt.Sprintf("%T", out), func(t *testing.T) {
			_, ok := out.(fx.Owner)
			require.False(t, ok)
		})
	}
}
