// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// An 0x address owns a P-chain UTXO and spends it with a warp message the
// trusted helper sends on its behalf instead of a secp256k1 signature.
func TestWarpCredentialSpendsUTXO(t *testing.T) {
	require := require.New(t)
	vm, _, _ := defaultVM(t, upgradetest.Latest)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

	// One primary network validator signs warp messages.
	sk, err := localsigner.New()
	require.NoError(err)
	vm.ctx.ValidatorState.(*validatorstest.State).GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{
			ids.GenerateTestNodeID(): {PublicKey: sk.PublicKey(), Weight: 1},
		}, nil
	}

	// Fund an EVM-style owner (any 20 bytes).
	owner := ids.ShortID{0xde, 0xad, 0xbe, 0xef}
	helper := ids.ShortID{0xca, 0xfe}
	vm.fx.(*secp256k1fx.Fx).WarpHelpers = set.Of(helper)
	wallet := newWallet(t, vm, walletConfig{})
	fundTx, err := wallet.IssueBaseTx([]*avax.TransferableOutput{{
		Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:          100 * units.MilliAvax,
			OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{owner}},
		},
	}})
	require.NoError(err)
	vm.ctx.Lock.Unlock()
	require.NoError(vm.issueTxFromRPC(fundTx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	// Spend it: the owner never signs, the credential is a warp message.
	unsigned := &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    vm.ctx.NetworkID,
		BlockchainID: vm.ctx.ChainID,
		Ins: []*avax.TransferableInput{{
			UTXOID: avax.UTXOID{TxID: fundTx.ID(), OutputIndex: 0},
			Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
			In:     &secp256k1fx.TransferInput{Amt: 100 * units.MilliAvax, Input: secp256k1fx.Input{SigIndices: []uint32{0}}},
		}},
		Outs: []*avax.TransferableOutput{{
			Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt:          50 * units.MilliAvax,
				OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{ids.GenerateTestShortID()}},
			},
		}},
	}}
	require.NoError((&txs.Tx{Unsigned: unsigned}).Initialize(txs.Codec))

	// The C-chain block that emitted the message; the P-chain ignores it.
	emitHeight := binary.BigEndian.AppendUint64(nil, 7)
	newTx := func(sender, claimedOwner ids.ShortID) *txs.Tx {
		call, err := payload.NewAddressedCall(sender[:], slices.Concat(claimedOwner[:], emitHeight, unsigned.Bytes()))
		require.NoError(err)
		unsignedMsg, err := warp.NewUnsignedMessage(vm.ctx.NetworkID, vm.ctx.CChainID, call.Bytes())
		require.NoError(err)
		sig, err := sk.Sign(unsignedMsg.Bytes())
		require.NoError(err)
		msg, err := warp.NewMessage(unsignedMsg, &warp.BitSetSignature{
			Signers:   set.NewBits(0).Bytes(),
			Signature: [bls.SignatureLen]byte(bls.SignatureToBytes(sig)),
		})
		require.NoError(err)
		tx := &txs.Tx{Unsigned: unsigned, Creds: []verify.Verifiable{&secp256k1fx.WarpCredential{Message: msg.Bytes()}}}
		require.NoError(tx.Initialize(txs.Codec))
		return tx
	}

	vm.ctx.Lock.Unlock()
	// The owner sending directly, and the helper naming a stranger.
	require.ErrorIs(vm.issueTxFromRPC(newTx(owner, owner)), secp256k1fx.ErrWrongWarpSourceAddr)
	require.ErrorIs(vm.issueTxFromRPC(newTx(helper, ids.GenerateTestShortID())), secp256k1fx.ErrWrongWarpSourceAddr)

	spendTx := newTx(helper, owner)
	require.NoError(vm.issueTxFromRPC(spendTx))
	vm.ctx.Lock.Lock()
	require.NoError(buildAndAcceptStandardBlock(vm))

	_, txStatus, err := vm.state.GetTx(spendTx.ID())
	require.NoError(err)
	require.Equal(status.Committed, txStatus)
	_, err = vm.state.GetUTXO(spendTx.Unsigned.InputIDs().List()[0])
	require.ErrorIs(err, database.ErrNotFound)
}
