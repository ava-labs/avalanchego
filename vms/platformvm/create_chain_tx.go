// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"
	"unicode"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	errInvalidVMID             = errors.New("invalid VM ID")
	errFxIDsNotSortedAndUnique = errors.New("feature extensions IDs must be sorted and unique")
	errNameTooLong             = errors.New("name too long")
	errGenesisTooLong          = errors.New("genesis too long")
	errIllegalNameCharacter    = errors.New("illegal name character")

	_ UnsignedDecisionTx = &UnsignedCreateChainTx{}
)

const (
	maxNameLen    = 128
	maxGenesisLen = units.MiB
)

// UnsignedCreateChainTx is an unsigned CreateChainTx
type UnsignedCreateChainTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// ID of the Subnet that validates this blockchain
	SubnetID ids.ID `serialize:"true" json:"subnetID"`
	// A human readable name for the chain; need not be unique
	ChainName string `serialize:"true" json:"chainName"`
	// ID of the VM running on the new chain
	VMID ids.ID `serialize:"true" json:"vmID"`
	// IDs of the feature extensions running on the new chain
	FxIDs []ids.ID `serialize:"true" json:"fxIDs"`
	// Byte representation of genesis state of the new chain
	GenesisData []byte `serialize:"true" json:"genesisData"`
	// Authorizes this blockchain to be added to this subnet
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

func (tx *UnsignedCreateChainTx) InputUTXOs() ids.Set { return nil }

func (tx *UnsignedCreateChainTx) AtomicOperations() (ids.ID, *atomic.Requests, error) {
	return ids.ID{}, nil, nil
}

func (tx *UnsignedCreateChainTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.SubnetID == constants.PrimaryNetworkID:
		return errDSCantValidate
	case len(tx.ChainName) > maxNameLen:
		return errNameTooLong
	case tx.VMID == ids.Empty:
		return errInvalidVMID
	case !ids.IsSortedAndUniqueIDs(tx.FxIDs):
		return errFxIDsNotSortedAndUnique
	case len(tx.GenesisData) > maxGenesisLen:
		return errGenesisTooLong
	}

	for _, r := range tx.ChainName {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsNumber(r) || r == ' ') {
			return errIllegalNameCharacter
		}
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}
	if err := tx.SubnetAuth.Verify(); err != nil {
		return err
	}

	tx.syntacticallyVerified = true
	return nil
}

// Attempts to verify this transaction with the provided state.
func (tx *UnsignedCreateChainTx) SemanticVerify(vm *VM, parentState MutableState, stx *Tx) error {
	vs := newVersionedState(
		parentState,
		parentState.CurrentStakerChainState(),
		parentState.PendingStakerChainState(),
	)
	_, err := tx.Execute(vm, vs, stx)
	return err
}

// Execute this transaction.
func (tx *UnsignedCreateChainTx) Execute(
	vm *VM,
	vs VersionedState,
	stx *Tx,
) (
	func() error,
	error,
) {
	// Make sure this transaction is well formed.
	if len(stx.Creds) == 0 {
		return nil, errWrongNumberOfCredentials
	}

	if err := tx.SyntacticVerify(vm.ctx); err != nil {
		return nil, err
	}

	// Select the credentials for each purpose
	baseTxCredsLen := len(stx.Creds) - 1
	baseTxCreds := stx.Creds[:baseTxCredsLen]
	subnetCred := stx.Creds[baseTxCredsLen]

	// Verify the flowcheck
	timestamp := vs.GetTimestamp()
	createBlockchainTxFee := vm.getCreateBlockchainTxFee(timestamp)
	if err := vm.semanticVerifySpend(vs, tx, tx.Ins, tx.Outs, baseTxCreds, createBlockchainTxFee, vm.ctx.AVAXAssetID); err != nil {
		return nil, err
	}

	subnetIntf, _, err := vs.GetTx(tx.SubnetID)
	if err == database.ErrNotFound {
		return nil, fmt.Errorf("%s isn't a known subnet", tx.SubnetID)
	}
	if err != nil {
		return nil, err
	}

	subnet, ok := subnetIntf.UnsignedTx.(*UnsignedCreateSubnetTx)
	if !ok {
		return nil, fmt.Errorf("%s isn't a subnet", tx.SubnetID)
	}

	// Verify that this chain is authorized by the subnet
	if err := vm.fx.VerifyPermission(tx, tx.SubnetAuth, subnetCred, subnet.Owner); err != nil {
		return nil, err
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
) (*Tx, error) {
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
	utx := &UnsignedCreateChainTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
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
	}
	tx := &Tx{UnsignedTx: utx}
	if err := tx.Sign(Codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.SyntacticVerify(vm.ctx)
}

func (vm *VM) getCreateBlockchainTxFee(t time.Time) uint64 {
	if t.Before(vm.ApricotPhase3Time) {
		return vm.CreateAssetTxFee
	}
	return vm.CreateBlockchainTxFee
}
