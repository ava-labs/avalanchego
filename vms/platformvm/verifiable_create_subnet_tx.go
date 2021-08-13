// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/platformcodec"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ VerifiableUnsignedDecisionTx = VerifiableUnsignedCreateSubnetTx{}

// VerifiableUnsignedCreateSubnetTx is an unsigned CreateChainTx
type VerifiableUnsignedCreateSubnetTx struct {
	*transactions.UnsignedCreateSubnetTx `serialize:"true"`
}

// SemanticVerify returns nil if [tx] is valid given the state in [db]
func (tx VerifiableUnsignedCreateSubnetTx) SemanticVerify(
	vm *VM,
	vs VersionedState,
	stx *transactions.SignedTx,
) (
	func() error,
	TxError,
) {
	// Make sure this transactions.is well formed.
	if err := tx.Verify(vm.ctx, platformcodec.Codec, vm.CreationTxFee, vm.ctx.AVAXAssetID); err != nil {
		return nil, permError{err}
	}

	// Verify the flowcheck
	if err := vm.semanticVerifySpend(vs, tx, tx.Ins, tx.Outs, stx.Creds, vm.CreationTxFee, vm.ctx.AVAXAssetID); err != nil {
		return nil, err
	}

	// Consume the UTXOS
	consumeInputs(vs, tx.Ins)
	// Produce the UTXOS
	txID := tx.ID()
	produceOutputs(vs, txID, vm.ctx.AVAXAssetID, tx.Outs)
	// Attempt to the new chain to the database
	vs.AddSubnet(stx)

	return nil, nil
}

// [controlKeys] must be unique. They will be sorted by this method.
// If [controlKeys] is nil, [tx.Controlkeys] will be an empty list.
func (vm *VM) newCreateSubnetTx(
	threshold uint32, // [threshold] of [ownerAddrs] needed to manage this subnet
	ownerAddrs []ids.ShortID, // control addresses for the new subnet
	keys []*crypto.PrivateKeySECP256K1R, // pay the fee
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*transactions.SignedTx, error) {
	ins, outs, _, signers, err := vm.stake(keys, 0, vm.CreationTxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Sort control addresses
	ids.SortShortIDs(ownerAddrs)

	// Create the tx
	utx := VerifiableUnsignedCreateSubnetTx{
		UnsignedCreateSubnetTx: &transactions.UnsignedCreateSubnetTx{
			BaseTx: transactions.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    vm.ctx.NetworkID,
				BlockchainID: vm.ctx.ChainID,
				Ins:          ins,
				Outs:         outs,
			}},
			Owner: &secp256k1fx.OutputOwners{
				Threshold: threshold,
				Addrs:     ownerAddrs,
			},
		},
	}

	tx := &transactions.SignedTx{UnsignedTx: utx}
	if err := tx.Sign(platformcodec.Codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(vm.ctx, platformcodec.Codec, vm.CreationTxFee, vm.ctx.AVAXAssetID)
}
