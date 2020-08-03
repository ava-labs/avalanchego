// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"unicode"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errInvalidVMID                   = errors.New("invalid VM ID")
	errFxIDsNotSortedAndUnique       = errors.New("feature extensions IDs must be sorted and unique")
	errControlSigsNotSortedAndUnique = errors.New("control signatures must be sorted and unique")
	errNameTooLong                   = errors.New("name too long")
	errGenesisTooLong                = errors.New("genesis too long")
	errIllegalNameCharacter          = errors.New("illegal name character")
)

const (
	maxNameLen    = 1 << 7
	maxGenesisLen = 1 << 20
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
	// Auth that will be allowing this validator into the network
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *UnsignedCreateChainTx) initialize(vm *VM, bytes []byte) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
	tx.vm = vm
	tx.bytes = bytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(bytes))
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedCreateChainTx: %w", err)
	}
	return nil
}

// Verify this transaction is well-formed
func (tx *UnsignedCreateChainTx) Verify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.SubnetID.IsZero():
		return errNoSubnetID
	case tx.SubnetID.Equals(constants.DefaultSubnetID):
		return errDSCantValidate
	case len(tx.ChainName) > maxNameLen:
		return errNameTooLong
	case tx.VMID.IsZero():
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

	if err := verify.All(&tx.BaseTx, tx.SubnetAuth); err != nil {
		return err
	}
	if err := syntacticVerifySpend(tx.Ins, tx.Outs, nil, 0, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}

	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedCreateChainTx) SemanticVerify(
	db database.Database,
	stx *DecisionTx,
) (
	func() error,
	TxError,
) {
	// Make sure this transaction is well formed.
	if len(stx.Credentials) == 0 {
		return nil, permError{errWrongNumberOfCredentials}
	}
	if err := tx.Verify(); err != nil {
		return nil, permError{err}
	}

	// Select the credentials for each purpose
	baseTxCredsLen := len(stx.Credentials) - 1
	baseTxCreds := stx.Credentials[:baseTxCredsLen]
	subnetCred := stx.Credentials[baseTxCredsLen]

	// Verify the flowcheck
	if err := tx.vm.semanticVerifySpend(db, tx, tx.Ins, tx.Outs, baseTxCreds); err != nil {
		return nil, err
	}

	txID := tx.ID()

	// Consume the UTXOS
	if err := tx.vm.consumeInputs(db, tx.Ins); err != nil {
		return nil, tempError{err}
	}
	// Produce the UTXOS
	if err := tx.vm.produceOutputs(db, txID, tx.Outs); err != nil {
		return nil, tempError{err}
	}

	// Verify that this chain is authorized by the subnet
	subnet, err := tx.vm.getSubnet(db, tx.SubnetID)
	if err != nil {
		return nil, err
	}
	unsignedSubnet := subnet.UnsignedDecisionTx.(*UnsignedCreateSubnetTx)
	if err := tx.vm.fx.VerifyPermission(tx, tx.SubnetAuth, subnetCred, unsignedSubnet.Owner); err != nil {
		return nil, permError{err}
	}

	// Attempt to add the new chain to the database
	currentChains, sErr := tx.vm.getChains(db) // chains that currently exist
	if err != nil {
		return nil, tempError{fmt.Errorf("couldn't get list of blockchains: %w", sErr)}
	}
	for _, chain := range currentChains {
		if chain.ID().Equals(tx.ID()) {
			return nil, permError{fmt.Errorf("chain %s already exists", chain.ID())}
		}
	}
	currentChains = append(currentChains, stx) // add this new chain
	if err := tx.vm.putChains(db, currentChains); err != nil {
		return nil, tempError{err}
	}

	// If this proposal is committed and this node is a member of the
	// subnet that validates the blockchain, create the blockchain
	onAccept := func() error { tx.vm.createChain(stx); return nil }
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
) (*DecisionTx, error) {
	ins, outs, _, signers, err := vm.spend(vm.DB, keys, 0, vm.txFee)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	subnetAuth, subnetSigners, err := vm.authorize(vm.DB, subnetID, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't authorize tx's subnet restrictions: %w", err)
	}
	signers = append(signers, subnetSigners)

	// Sort the provided fxIDs
	ids.SortIDs(fxIDs)

	// Create the tx
	tx := &DecisionTx{UnsignedDecisionTx: &UnsignedCreateChainTx{
		BaseTx: BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
			Ins:          ins,
			Outs:         outs,
		},
		SubnetID:    subnetID,
		ChainName:   chainName,
		VMID:        vmID,
		FxIDs:       fxIDs,
		GenesisData: genesisData,
		SubnetAuth:  subnetAuth,
	}}
	return tx, vm.signDecisionTx(tx, signers)
}
