// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/platformcodec"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions"
)

var _ VerifiableUnsignedDecisionTx = VerifiableUnsignedCreateChainTx{}

// VerifiableUnsignedCreateChainTx is an unsigned CreateChainTx
type VerifiableUnsignedCreateChainTx struct {
	*transactions.UnsignedCreateChainTx `serialize:"true"`
}

// SemanticVerify this transaction is valid.
func (tx VerifiableUnsignedCreateChainTx) SemanticVerify(
	vm *VM,
	vs VersionedState,
	stx *transactions.SignedTx,
) (
	func() error,
	TxError,
) {
	// Make sure this transaction is well formed.
	if len(stx.Creds) == 0 {
		return nil, permError{errWrongNumberOfCredentials}
	}

	timestamp := vs.GetTimestamp()
	createBlockchainTxFee := vm.getCreateBlockchainTxFee(timestamp)
	syntacticCtx := transactions.DecisionTxSyntacticVerificationContext{
		Ctx:        vm.ctx,
		C:          platformcodec.Codec,
		FeeAmount:  createBlockchainTxFee,
		FeeAssetID: vm.ctx.AVAXAssetID,
	}
	if err := tx.SyntacticVerify(syntacticCtx); err != nil {
		return nil, permError{err}
	}

	// Select the credentials for each purpose
	baseTxCredsLen := len(stx.Creds) - 1
	baseTxCreds := stx.Creds[:baseTxCredsLen]
	subnetCred := stx.Creds[baseTxCredsLen]

	// Verify the flowcheck
	if err := vm.semanticVerifySpend(vs, tx, tx.Ins, tx.Outs, baseTxCreds, createBlockchainTxFee, vm.ctx.AVAXAssetID); err != nil {
		return nil, err
	}

	subnetIntf, _, err := vs.GetTx(tx.SubnetID)
	if err == database.ErrNotFound {
		return nil, permError{
			fmt.Errorf("%s isn't a known subnet", tx.SubnetID),
		}
	}
	if err != nil {
		return nil, tempError{err}
	}

	subnet, ok := subnetIntf.UnsignedTx.(VerifiableUnsignedCreateSubnetTx)
	if !ok {
		return nil, permError{
			fmt.Errorf("%s isn't a subnet", tx.SubnetID),
		}
	}

	// Verify that this chain is authorized by the subnet
	if err := vm.fx.VerifyPermission(tx, tx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
		return nil, permError{err}
	}

	// Consume the UTXOS
	consumeInputs(vs, tx.Ins)
	// Produce the UTXOS
	txID := tx.ID()
	produceOutputs(vs, txID, vm.ctx.AVAXAssetID, tx.Outs)
	// Attempt to the new chain to the database
	vs.AddChain(stx)

	// If this proposal is committed and this node is a member of the
	// subnet that validates the blockchain, create the blockchain
	onAccept := func() error { return vm.createChain(stx) }
	return onAccept, nil
}

// Create a new transaction
func (vm *VM) newCreateChainTx(
	subnetID ids.ID, // ID of the subnet that validates the new chain
	genesisData []byte, // Byte repr. of genesis state of the new chain
	vmID ids.ID, // VM this chain runs
	fxIDs []ids.ID, // fxs this chain supports
	chainName string, // Name of the chain
	keys []*crypto.PrivateKeySECP256K1R, // Keys to sign the tx
	changeAddr ids.ShortID, // Address to send change to, if there is any
) (*transactions.SignedTx, error) {
	timestamp := vm.internalState.GetTimestamp()
	createBlockchainTxFee := vm.getCreateBlockchainTxFee(timestamp)
	ins, outs, _, signers, err := vm.stake(keys, 0, createBlockchainTxFee, changeAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	subnetAuth, subnetSigners, err := vm.authorize(vm.internalState, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}
	signers = append(signers, subnetSigners)

	// Sort the provided fxIDs
	ids.SortIDs(fxIDs)

	// Create the tx
	utx := VerifiableUnsignedCreateChainTx{
		UnsignedCreateChainTx: &transactions.UnsignedCreateChainTx{
			BaseTx: transactions.BaseTx{BaseTx: avax.BaseTx{
				NetworkID:    vm.ctx.NetworkID,
				BlockchainID: vm.ctx.ChainID,
				Ins:          ins,
				Outs:         outs,
			}},
			SubnetID:    subnetID,
			ChainName:   chainName,
			VMID:        vmID,
			FxIDs:       fxIDs,
			GenesisData: genesisData,
			SubnetAuth:  subnetAuth,
		},
	}
	tx := &transactions.SignedTx{UnsignedTx: utx}
	if err := tx.Sign(platformcodec.Codec, signers); err != nil {
		return nil, err
	}

	syntacticCtx := transactions.DecisionTxSyntacticVerificationContext{
		Ctx:        vm.ctx,
		C:          platformcodec.Codec,
		FeeAmount:  createBlockchainTxFee,
		FeeAssetID: vm.ctx.AVAXAssetID,
	}
	return tx, utx.SyntacticVerify(syntacticCtx)
}

func (vm *VM) getCreateBlockchainTxFee(t time.Time) uint64 {
	if t.Before(vm.ApricotPhase3Time) {
		return vm.CreateAssetTxFee
	}
	return vm.CreateBlockchainTxFee
}
