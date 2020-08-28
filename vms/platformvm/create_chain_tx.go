// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"unicode"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/components/verify"
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

// Verify this transaction is well-formed
func (tx *UnsignedCreateChainTx) Verify(
	ctx *snow.Context,
	c codec.Codec,
	feeAmount uint64,
	feeAssetID ids.ID,
) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.SubnetID.IsZero():
		return errNoSubnetID
	case tx.SubnetID.Equals(constants.PrimaryNetworkID):
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

	if err := tx.BaseTx.Verify(ctx, c); err != nil {
		return err
	}
	if err := tx.SubnetAuth.Verify(); err != nil {
		return err
	}

	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedCreateChainTx) SemanticVerify(
	vm *VM,
	db database.Database,
	stx *Tx,
) (
	func() error,
	TxError,
) {
	// Make sure this transaction is well formed.
	if len(stx.Creds) == 0 {
		return nil, permError{errWrongNumberOfCredentials}
	}
	if err := tx.Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID); err != nil {
		return nil, permError{err}
	}

	// Select the credentials for each purpose
	baseTxCredsLen := len(stx.Creds) - 1
	baseTxCreds := stx.Creds[:baseTxCredsLen]
	subnetCred := stx.Creds[baseTxCredsLen]

	// Verify the flowcheck
	if err := vm.semanticVerifySpend(db, tx, tx.Ins, tx.Outs, baseTxCreds, vm.txFee, vm.Ctx.AVAXAssetID); err != nil {
		return nil, err
	}

	txID := tx.ID()

	// Consume the UTXOS
	if err := vm.consumeInputs(db, tx.Ins); err != nil {
		return nil, tempError{err}
	}
	// Produce the UTXOS
	if err := vm.produceOutputs(db, txID, tx.Outs); err != nil {
		return nil, tempError{err}
	}

	// Verify that this chain is authorized by the subnet
	subnet, err := vm.getSubnet(db, tx.SubnetID)
	if err != nil {
		return nil, err
	}
	unsignedSubnet := subnet.UnsignedTx.(*UnsignedCreateSubnetTx)
	if err := vm.fx.VerifyPermission(tx, tx.SubnetAuth, subnetCred, unsignedSubnet.Owner); err != nil {
		return nil, permError{err}
	}

	// Attempt to add the new chain to the database
	currentChains, sErr := vm.getChains(db) // chains that currently exist
	if err != nil {
		return nil, tempError{fmt.Errorf("couldn't get list of blockchains: %w", sErr)}
	}
	for _, chain := range currentChains {
		if chain.ID().Equals(tx.ID()) {
			return nil, permError{fmt.Errorf("chain %s already exists", chain.ID())}
		}
	}
	currentChains = append(currentChains, stx) // add this new chain
	if err := vm.putChains(db, currentChains); err != nil {
		return nil, tempError{err}
	}

	// If this proposal is committed and this node is a member of the
	// subnet that validates the blockchain, create the blockchain
	onAccept := func() error { vm.createChain(stx); return nil }
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
) (*Tx, error) {
	ins, outs, _, signers, err := vm.stake(vm.DB, keys, 0, vm.txFee)
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
	utx := &UnsignedCreateChainTx{
		BaseTx: BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
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
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID)
}
