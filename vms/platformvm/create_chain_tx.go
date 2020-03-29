// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/chains"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
)

var (
	errInvalidVMID             = errors.New("invalid VM ID")
	errFxIDsNotSortedAndUnique = errors.New("feature extensions IDs must be sorted and unique")
)

// UnsignedCreateChainTx is an unsigned CreateChainTx
type UnsignedCreateChainTx struct {
	// ID of the network this blockchain exists on
	NetworkID uint32 `serialize:"true"`

	// Next unused nonce of account paying the transaction fee for this transaction.
	// Currently unused, as there are no tx fees.
	Nonce uint64 `serialize:"true"`

	// A human readable name for the chain; need not be unique
	ChainName string `serialize:"true"`

	// ID of the VM running on the new chain
	VMID ids.ID `serialize:"true"`

	// IDs of the feature extensions running on the new chain
	FxIDs []ids.ID `serialize:"true"`

	// Byte representation of state of the new chain
	GenesisData []byte `serialize:"true"`
}

// CreateChainTx is a proposal to create a chain
type CreateChainTx struct {
	UnsignedCreateChainTx `serialize:"true"`

	Sig [crypto.SECP256K1RSigLen]byte `serialize:"true"`

	vm    *VM
	id    ids.ID
	key   crypto.PublicKey // public key of transaction signer
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

// Key returns the public key of the signer of this transaction
// Precondition: tx.Verify() has been called and returned nil
func (tx *CreateChainTx) Key() crypto.PublicKey { return tx.key }

// Bytes returns the byte representation of a CreateChainTx
func (tx *CreateChainTx) Bytes() []byte { return tx.bytes }

// SyntacticVerify this transaction is well-formed
// Also populates [tx.Key] with the public key that signed this transaction
func (tx *CreateChainTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.key != nil:
		return nil // Only verify the transaction once
	case tx.NetworkID != tx.vm.Ctx.NetworkID: // verify the transaction is on this network
		return errWrongNetworkID
	case tx.id.IsZero():
		return errInvalidID
	case tx.VMID.IsZero():
		return errInvalidVMID
	case !ids.IsSortedAndUniqueIDs(tx.FxIDs):
		return errFxIDsNotSortedAndUnique
	}

	unsignedIntf := interface{}(&tx.UnsignedCreateChainTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // byte repr of unsigned tx
	if err != nil {
		return err
	}

	key, err := tx.vm.factory.RecoverPublicKey(unsignedBytes, tx.Sig[:])
	if err != nil {
		return err
	}
	tx.key = key

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
	account, err := tx.vm.getAccount(db, tx.Key().Address())
	if err != nil {
		return nil, err
	}
	account, err = account.Remove(0, tx.Nonce)
	if err != nil {
		return nil, err
	}
	if err := tx.vm.putAccount(db, account); err != nil {
		return nil, err
	}

	// If this proposal is committed, create the new blockchain using the chain manager
	onAccept := func() {
		chainParams := chains.ChainParameters{
			ID:          tx.ID(),
			GenesisData: tx.GenesisData,
			VMAlias:     tx.VMID.String(),
		}
		for _, fxID := range tx.FxIDs {
			chainParams.FxAliases = append(chainParams.FxAliases, fxID.String())
		}
		// TODO: Not sure how else to make this not nil pointer error during tests
		if tx.vm.chainManager != nil {
			tx.vm.chainManager.CreateChain(chainParams)
		}
	}

	return onAccept, nil
}

// We use this type so we can serialize a list of *CreateChainTx
// by defining a Bytes method on it
type createChainList []*CreateChainTx

// Bytes returns the byte representation of a list of *CreateChainTx
func (chains createChainList) Bytes() []byte {
	bytes, _ := Codec.Marshal(chains)
	return bytes
}

func (vm *VM) newCreateChainTx(nonce uint64, genesisData []byte, vmID ids.ID, fxIDs []ids.ID, chainName string, networkID uint32, key *crypto.PrivateKeySECP256K1R) (*CreateChainTx, error) {
	tx := &CreateChainTx{
		UnsignedCreateChainTx: UnsignedCreateChainTx{
			NetworkID:   networkID,
			Nonce:       nonce,
			GenesisData: genesisData,
			VMID:        vmID,
			FxIDs:       fxIDs,
			ChainName:   chainName,
		},
	}

	unsignedIntf := interface{}(&tx.UnsignedCreateChainTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // Byte repr. of unsigned transaction
	if err != nil {
		return nil, err
	}

	sig, err := key.Sign(unsignedBytes)
	if err != nil {
		return nil, err
	}
	copy(tx.Sig[:], sig)

	return tx, tx.initialize(vm)
}
