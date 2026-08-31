// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpauth

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func TestWrapCredentialCount(t *testing.T) {
	base := goBase(4) // three inputs
	tests := []struct {
		tx       txs.UnsignedTx
		numCreds int
	}{
		{&txs.BaseTx{BaseTx: base.BaseTx}, 3},
		{&txs.CreateSubnetTx{BaseTx: base, Owner: &goOneOwner}, 3},
		{&txs.CreateChainTx{BaseTx: base, SubnetAuth: goAuth()}, 4},
		{&txs.AddSubnetValidatorTx{BaseTx: base, SubnetAuth: goAuth()}, 4},
		{&txs.RemoveSubnetValidatorTx{BaseTx: base, SubnetAuth: goAuth()}, 4},
		{&txs.AddPermissionlessValidatorTx{BaseTx: base, Signer: &goPoP, ValidatorRewardsOwner: &goOneOwner, DelegatorRewardsOwner: &goOneOwner}, 3},
		{&txs.AddPermissionlessDelegatorTx{BaseTx: base, DelegationRewardsOwner: &goOneOwner}, 3},
		{&txs.TransferSubnetOwnershipTx{BaseTx: base, SubnetAuth: goAuth(), Owner: &goOneOwner}, 4},
		{&txs.ImportTx{BaseTx: base, ImportedInputs: goIns([]utxo{{TxID: ids.ID{0x12}, Amount: 1}})}, 4},
		{&txs.ExportTx{BaseTx: base}, 3},
		{&txs.ConvertSubnetToL1Tx{BaseTx: base, SubnetAuth: goAuth()}, 4},
		{&txs.RegisterL1ValidatorTx{BaseTx: base}, 3},
		{&txs.SetL1ValidatorWeightTx{BaseTx: base}, 3},
		{&txs.IncreaseL1ValidatorBalanceTx{BaseTx: base}, 3},
		{&txs.DisableL1ValidatorTx{BaseTx: base, DisableAuth: goAuth()}, 4},
		{&txs.AddAutoRenewedValidatorTx{BaseTx: base, Signer: &goPoP, ValidatorRewardsOwner: &goOneOwner, DelegatorRewardsOwner: &goOneOwner, ValidatorAuthority: &goOneOwner}, 3},
		{&txs.SetAutoRenewedValidatorConfigTx{BaseTx: base, Auth: goAuth()}, 4},
	}
	for _, test := range tests {
		unsigned := test.tx
		txBytes, err := txs.Codec.Marshal(txs.CodecVersion, &unsigned)
		require.NoError(t, err)
		call, err := payload.NewAddressedCall(ownerA[:], slices.Concat(owner[:], heightBytes, txBytes))
		require.NoError(t, err)
		unsignedMsg, err := warp.NewUnsignedMessage(constants.UnitTestID, ids.Empty, call.Bytes())
		require.NoError(t, err)
		msg, err := warp.NewMessage(unsignedMsg, &warp.BitSetSignature{})
		require.NoError(t, err)

		tx, gotOwner, err := Wrap(msg.Bytes())
		require.NoError(t, err, "%T", test.tx)
		require.Equal(t, owner, gotOwner)
		require.Len(t, tx.Creds, test.numCreds, "%T", test.tx)
		for _, cred := range tx.Creds {
			require.Equal(t, &secp256k1fx.WarpCredential{Message: msg.Bytes()}, cred)
		}
		require.Equal(t, txBytes, tx.Unsigned.Bytes())
	}
}
