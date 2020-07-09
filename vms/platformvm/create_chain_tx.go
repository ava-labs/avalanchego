// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

var (
	errInvalidVMID                   = errors.New("invalid VM ID")
	errFxIDsNotSortedAndUnique       = errors.New("feature extensions IDs must be sorted and unique")
	errControlSigsNotSortedAndUnique = errors.New("control signatures must be sorted and unique")
)

// UnsignedCreateChainTx is an unsigned CreateChainTx
type UnsignedCreateChainTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// ID of the Subnet that validates this blockchain
	SubnetID ids.ID `serialize:"true"`
	// A human readable name for the chain; need not be unique
	ChainName string `serialize:"true"`
	// ID of the VM running on the new chain
	VMID ids.ID `serialize:"true"`
	// IDs of the feature extensions running on the new chain
	FxIDs []ids.ID `serialize:"true"`
	// Byte representation of genesis state of the new chain
	GenesisData []byte `serialize:"true"`
}

// CreateChainTx is a proposal to create a chain
type CreateChainTx struct {
	UnsignedCreateChainTx `serialize:"true"`
	// Credentials that authorize the inputs to spend the corresponding outputs
	Credentials []verify.Verifiable `serialize:"true"`
	// Signatures from Subnet's control keys
	// Should not empty slice, not nil, if there are no control sigs
	ControlSigs [][crypto.SECP256K1RSigLen]byte `serialize:"true"`
}

// Creds returns this transactions credentials
func (tx *CreateChainTx) Creds() []verify.Verifiable {
	return tx.Credentials
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *CreateChainTx) initialize(vm *VM) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
	tx.vm = vm
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedCreateChainTx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedCreateChainTx: %w", err)
	}
	tx.bytes, err = Codec.Marshal(tx) // byte representation of the signed transaction
	if err != nil {
		return fmt.Errorf("couldn't marshal CreateChainTx: %w", err)
	}
	tx.id = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return err
}

// SyntacticVerify this transaction is well-formed
// Also populates [tx.Key] with the public key that signed this transaction
func (tx *CreateChainTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.NetworkID != tx.vm.Ctx.NetworkID: // verify the transaction is on this network
		return errWrongNetworkID
	case tx.id.IsZero():
		return errInvalidID
	case tx.VMID.IsZero():
		return errInvalidVMID
	case tx.SubnetID.Equals(DefaultSubnetID):
		return errDSCantValidate
	case !ids.IsSortedAndUniqueIDs(tx.FxIDs):
		return errFxIDsNotSortedAndUnique
	case !crypto.IsSortedAndUniqueSECP2561RSigs(tx.ControlSigs):
		return errControlSigsNotSortedAndUnique
	}
	if err := syntacticVerifySpend(tx, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *CreateChainTx) SemanticVerify(db database.Database) (func(), error) {
	if err := tx.SyntacticVerify(); err != nil {
		return nil, err
	}

	currentChains, err := tx.vm.getChains(db) // chains that currently exist
	if err != nil {
		return nil, fmt.Errorf("couldn't get list of blockchains: %w", err)
	}
	for _, chain := range currentChains {
		if chain.ID().Equals(tx.ID()) {
			return nil, fmt.Errorf("chain %s already exists", chain.ID())
		}
	}
	currentChains = append(currentChains, tx) // add this new chain
	if err := tx.vm.putChains(db, currentChains); err != nil {
		return nil, err
	}

	// Verify inputs/outputs and update the UTXO set
	if err := tx.vm.semanticVerifySpend(db, tx); err != nil {
		return nil, tempError{fmt.Errorf("couldn't verify tx: %w", err)}
	}

	// Verify that this transaction has sufficient control signatures
	subnets, err := tx.vm.getSubnets(db) // all subnets that exist
	if err != nil {
		return nil, err
	}
	var subnet *CreateSubnetTx // the subnet that will validate the new chain
	for _, sn := range subnets {
		if sn.id.Equals(tx.SubnetID) {
			subnet = sn
			break
		}
	}
	if subnet == nil {
		return nil, fmt.Errorf("subnet %s doesn't exist", tx.SubnetID)
	} else if len(tx.ControlSigs) != int(subnet.Threshold) {
		return nil, fmt.Errorf("expected tx to have %d control sigs but has %d", subnet.Threshold, len(tx.ControlSigs))
	}
	unsignedBytesHash := hashing.ComputeHash256(tx.unsignedBytes)
	// Each element is ID of key that signed this tx
	controlIDs := make([]ids.ShortID, len(tx.ControlSigs))
	for i, sig := range tx.ControlSigs {
		key, err := tx.vm.factory.RecoverHashPublicKey(unsignedBytesHash, sig[:])
		if err != nil {
			return nil, err
		}
		controlIDs[i] = key.Address()
	}

	// Verify each control signature on this tx is from a control key
	controlKeys := ids.ShortSet{}
	controlKeys.Add(subnet.ControlKeys...)
	for _, controlID := range controlIDs {
		if !controlKeys.Contains(controlID) {
			return nil, errors.New("tx has control signature from key not in subnet's ControlKeys")
		}
	}

	// If this proposal is committed and this node is a member of the
	// subnet that validates the blockchain, create the blockchain
	onAccept := func() { tx.vm.createChain(tx) }
	return onAccept, nil
}

// Create a new transaction
func (vm *VM) newCreateChainTx(
	subnetID ids.ID, // ID of the subnet that validates the new chain
	genesisData []byte, // Byte repr. of genesis state of the new chain
	vmID ids.ID, // VM this chain runs
	fxIDs []ids.ID, // fxs this chain supports
	chainName string, // Name of the chain
	controlKeys []*crypto.PrivateKeySECP256K1R, // Control keys for the subnet
	feeKeys []*crypto.PrivateKeySECP256K1R, // Pay the fee
) (*CreateChainTx, error) {

	// Get information about the subnet we're adding a chain to
	subnetInfo, err := vm.getSubnet(vm.DB, subnetID)
	if err != nil {
		return nil, fmt.Errorf("subnet %s doesn't exist", subnetID)
	}
	// Make sure we have enough of this subnet's control keys
	subnetControlKeys := ids.ShortSet{} // Subnet's
	for _, key := range subnetInfo.ControlKeys {
		subnetControlKeys.Add(key)
	}
	// [usableKeys] are the control keys that will sign this transaction
	usableKeys := make([]*crypto.PrivateKeySECP256K1R, 0, subnetInfo.Threshold)
	for _, key := range controlKeys {
		if subnetControlKeys.Contains(key.PublicKey().Address()) {
			usableKeys = append(usableKeys, key) // This key is useful
		}
		if len(usableKeys) > int(subnetInfo.Threshold) {
			break
		}
	}
	if len(usableKeys) != int(subnetInfo.Threshold) {
		return nil, fmt.Errorf("don't have enough control keys for subnet %s", subnetID)
	}

	// Calculate inputs, outputs, and keys used to sign this tx
	inputs, outputs, credKeys, err := vm.spend(vm.DB, vm.txFee, feeKeys)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the tx
	tx := &CreateChainTx{
		UnsignedCreateChainTx: UnsignedCreateChainTx{
			BaseTx: BaseTx{
				NetworkID:    vm.Ctx.NetworkID,
				BlockchainID: ids.Empty,
				Inputs:       inputs,
				Outputs:      outputs,
			},
			SubnetID:    subnetID,
			GenesisData: genesisData,
			VMID:        vmID,
			FxIDs:       fxIDs,
			ChainName:   chainName,
		},
	}

	// Generate byte repr. of unsigned transaction
	if tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedCreateChainTx)); err != nil {
		return nil, fmt.Errorf("couldn't marshal UnsignedAddNonDefaultSubnetValidatorTx: %w", err)
	}
	hash := hashing.ComputeHash256(tx.unsignedBytes)

	// Sign the tx with control keys
	tx.ControlSigs = make([][crypto.SECP256K1RSigLen]byte, len(controlKeys))
	for i, key := range usableKeys {
		sig, err := key.SignHash(hash)
		if err != nil {
			return nil, err
		}
		copy(tx.ControlSigs[i][:], sig)
	}
	crypto.SortSECP2561RSigs(tx.ControlSigs) // Sort the control signatures

	// Attach credentials that allow input UTXOs to be spent
	for _, inputKeys := range credKeys { // [inputKeys] are the keys used to authorize spend of an input
		cred := &secp256k1fx.Credential{}
		for _, key := range inputKeys {
			sig, err := key.SignHash(hash) // Sign hash(tx.unsignedBytes)
			if err != nil {
				return nil, fmt.Errorf("problem generating credential: %w", err)
			}
			sigArr := [crypto.SECP256K1RSigLen]byte{}
			copy(sigArr[:], sig)
			cred.Sigs = append(cred.Sigs, sigArr)
		}
		tx.Credentials = append(tx.Credentials, cred) // Attach credential to tx
	}

	return tx, tx.initialize(vm)
}
