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
)

var (
	errInvalidVMID                   = errors.New("invalid VM ID")
	errFxIDsNotSortedAndUnique       = errors.New("feature extensions IDs must be sorted and unique")
	errControlSigsNotSortedAndUnique = errors.New("control signatures must be sorted and unique")
)

// UnsignedCreateChainTx is an unsigned CreateChainTx
type UnsignedCreateChainTx struct {
	// ID of the network this blockchain exists on
	NetworkID uint32 `serialize:"true"`

	// ID of the Subnet that validates this blockchain
	SubnetID ids.ID `serialize:"true"`

	// Next unused nonce of account paying the transaction fee for this transaction.
	// Currently unused, as there are no tx fees.
	Nonce uint64 `serialize:"true"`

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

	// Address of the account that provides the transaction fee
	// Set in SemanticVerify
	PayerAddress ids.ShortID

	// Signatures from Subnet's control keys
	// Should not empty slice, not nil, if there are no control sigs
	ControlSigs [][crypto.SECP256K1RSigLen]byte `serialize:"true"`

	// Signature of key whose account provides the transaction fee
	PayerSig [crypto.SECP256K1RSigLen]byte `serialize:"true"`

	vm    *VM
	id    ids.ID
	bytes []byte
}

func (tx *CreateChainTx) initialize(vm *VM) error {
	tx.vm = vm
	txBytes, err := Codec.Marshal(tx) // byte repr. of the signed tx
	tx.bytes = txBytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(txBytes))
	return err
}

// ID of this transaction
func (tx *CreateChainTx) ID() ids.ID { return tx.id }

// Bytes returns the byte representation of a CreateChainTx
func (tx *CreateChainTx) Bytes() []byte { return tx.bytes }

// SyntacticVerify this transaction is well-formed
// Also populates [tx.Key] with the public key that signed this transaction
func (tx *CreateChainTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case !tx.PayerAddress.IsZero(): // Only verify the transaction once
		return nil
	case tx.NetworkID != tx.vm.Ctx.NetworkID: // verify the transaction is on this network
		return errWrongNetworkID
	case tx.id.IsZero():
		return errInvalidID
	case tx.VMID.IsZero():
		return errInvalidVMID
	case tx.SubnetID.Equals(defaultSubnetID):
		return errDSCantValidate
	case !ids.IsSortedAndUniqueIDs(tx.FxIDs):
		return errFxIDsNotSortedAndUnique
	case !crypto.IsSortedAndUniqueSECP2561RSigs(tx.ControlSigs):
		return errControlSigsNotSortedAndUnique
	}

	unsignedIntf := interface{}(&tx.UnsignedCreateChainTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // byte repr of unsigned tx
	if err != nil {
		return err
	}

	payerKey, err := tx.vm.factory.RecoverPublicKey(unsignedBytes, tx.PayerSig[:])
	if err != nil {
		return err
	}
	tx.PayerAddress = payerKey.Address()

	return nil
}

// SemanticVerify this transaction is valid.
func (tx *CreateChainTx) SemanticVerify(db database.Database) (func(), error) {
	if err := tx.SyntacticVerify(); err != nil {
		return nil, err
	}

	currentChains, err := tx.vm.getChains(db) // chains that currently exist
	if err != nil {
		return nil, errDBChains
	}
	for _, chain := range currentChains {
		if chain.ID().Equals(tx.ID()) {
			return nil, fmt.Errorf("chain with ID %s already exists", chain.ID())
		}
	}
	currentChains = append(currentChains, tx) // add this new chain
	if err := tx.vm.putChains(db, currentChains); err != nil {
		return nil, err
	}

	// Deduct tx fee from payer's account
	account, err := tx.vm.getAccount(db, tx.PayerAddress)
	if err != nil {
		return nil, err
	}
	// txFee is removed in account.Remove
	// TODO: Consider changing Remove to be parameterized on total amount (inc. tx fee) to remove
	account, err = account.Remove(0, tx.Nonce)
	if err != nil {
		return nil, err
	}
	if err := tx.vm.putAccount(db, account); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("there is no subnet with ID %s", tx.SubnetID)
	}
	if len(tx.ControlSigs) != int(subnet.Threshold) {
		return nil, fmt.Errorf("expected tx to have %d control sigs but has %d", subnet.Threshold, len(tx.ControlSigs))
	}

	unsignedIntf := interface{}(&tx.UnsignedCreateChainTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // Byte representation of the unsigned transaction
	if err != nil {
		return nil, err
	}
	unsignedBytesHash := hashing.ComputeHash256(unsignedBytes)

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
	onAccept := func() {
		tx.vm.createChain(tx)
	}

	return onAccept, nil
}

// We use this type so we can serialize a list of *CreateChainTx
// by defining a Bytes method on it
// Transactions must be syntactically verified before adding to createChainList
// to ensure that [createChainList] can always be marshalled
type createChainList []*CreateChainTx

// Bytes returns the byte representation of a list of *CreateChainTx
func (chains createChainList) Bytes() []byte {
	// Assumes the individual transactions have been syntactically verified
	// so that it can be safely marshalled.
	bytes, _ := Codec.Marshal(chains)
	return bytes
}

func (vm *VM) newCreateChainTx(nonce uint64, subnetID ids.ID, genesisData []byte,
	vmID ids.ID, fxIDs []ids.ID, chainName string, networkID uint32,
	controlKeys []*crypto.PrivateKeySECP256K1R,
	payerKey *crypto.PrivateKeySECP256K1R) (*CreateChainTx, error) {
	tx := &CreateChainTx{
		UnsignedCreateChainTx: UnsignedCreateChainTx{
			NetworkID:   networkID,
			SubnetID:    subnetID,
			Nonce:       nonce,
			GenesisData: genesisData,
			VMID:        vmID,
			FxIDs:       fxIDs,
			ChainName:   chainName,
		},
	}

	// Generate byte repr. of unsigned transaction
	unsignedIntf := interface{}(&tx.UnsignedCreateChainTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf)
	if err != nil {
		return nil, err
	}
	unsignedBytesHash := hashing.ComputeHash256(unsignedBytes)

	// Sign the tx with control keys
	tx.ControlSigs = make([][crypto.SECP256K1RSigLen]byte, len(controlKeys))
	for i, key := range controlKeys {
		sig, err := key.SignHash(unsignedBytesHash)
		if err != nil {
			return nil, err
		}
		copy(tx.ControlSigs[i][:], sig)
	}

	// Sort the control signatures
	crypto.SortSECP2561RSigs(tx.ControlSigs)

	// Sign with the payer key
	payerSig, err := payerKey.Sign(unsignedBytes)
	if err != nil {
		return nil, err
	}
	copy(tx.PayerSig[:], payerSig)

	return tx, tx.initialize(vm)
}
