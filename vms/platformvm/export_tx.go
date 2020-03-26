// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

// import (
// 	"fmt"

// 	"github.com/ava-labs/gecko/chains"
// 	"github.com/ava-labs/gecko/database"
// 	"github.com/ava-labs/gecko/ids"
// 	"github.com/ava-labs/gecko/utils/crypto"
// 	"github.com/ava-labs/gecko/utils/hashing"
// 	"github.com/ava-labs/gecko/vms/components/ava"
// )

// // UnsignedExportTx is an unsigned ExportTx
// type UnsignedExportTx struct {
// 	// ID of the network this blockchain exists on
// 	NetworkID uint32 `serialize:"true"`

// 	// Next unused nonce of account paying the transaction fee for this transaction.
// 	// Currently unused, as there are no tx fees.
// 	Nonce uint64 `serialize:"true"`

// 	Outs []*ava.TransferableOutput `serialize:"true"` // The outputs of this transaction
// }

// // ExportTx exports funds to the AVM
// type ExportTx struct {
// 	UnsignedExportTx `serialize:"true"`

// 	Sig [crypto.SECP256K1RSigLen]byte `serialize:"true"`

// 	vm    *VM
// 	id    ids.ID
// 	key   crypto.PublicKey // public key of transaction signer
// 	bytes []byte
// }

// func (tx *ExportTx) initialize(vm *VM) error {
// 	tx.vm = vm
// 	txBytes, err := Codec.Marshal(tx) // byte repr. of the signed tx
// 	tx.bytes = txBytes
// 	tx.id = ids.NewID(hashing.ComputeHash256Array(txBytes))
// 	return err
// }

// // ID of this transaction
// func (tx *ExportTx) ID() ids.ID { return tx.id }

// // Key returns the public key of the signer of this transaction
// // Precondition: tx.Verify() has been called and returned nil
// func (tx *ExportTx) Key() crypto.PublicKey { return tx.key }

// // Bytes returns the byte representation of a CreateChainTx
// func (tx *ExportTx) Bytes() []byte { return tx.bytes }

// // InputUTXOs returns an empty set
// func (tx *ExportTx) InputUTXOs() ids.Set { return ids.Set{} }

// // SyntacticVerify this transaction is well-formed
// // Also populates [tx.Key] with the public key that signed this transaction
// func (tx *ExportTx) SyntacticVerify() error {
// 	switch {
// 	case tx == nil:
// 		return errNilTx
// 	case tx.key != nil:
// 		return nil // Only verify the transaction once
// 	case tx.NetworkID != tx.vm.Ctx.NetworkID: // verify the transaction is on this network
// 		return errWrongNetworkID
// 	case tx.id.IsZero():
// 		return errInvalidID
// 	}

// 	unsignedIntf := interface{}(&tx.UnsignedImportTx)
// 	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // byte repr of unsigned tx
// 	if err != nil {
// 		return err
// 	}

// 	key, err := tx.vm.factory.RecoverPublicKey(unsignedBytes, tx.Sig[:])
// 	if err != nil {
// 		return err
// 	}
// 	tx.key = key

// 	return nil
// }

// // SemanticVerify this transaction is valid.
// func (tx *ExportTx) SemanticVerify(db database.Database) (func(), error) {
// 	if err := tx.SyntacticVerify(); err != nil {
// 		return nil, err
// 	}

// 	currentChains, err := tx.vm.getChains(db) // chains that currently exist
// 	if err != nil {
// 		return nil, errDBChains
// 	}
// 	for _, chain := range currentChains {
// 		if chain.ID().Equals(tx.ID()) {
// 			return nil, fmt.Errorf("chain with ID %s already exists", chain.ID())
// 		}
// 	}
// 	currentChains = append(currentChains, tx) // add this new chain
// 	if err := tx.vm.putChains(db, currentChains); err != nil {
// 		return nil, err
// 	}

// 	// Deduct tx fee from payer's account
// 	account, err := tx.vm.getAccount(db, tx.Key().Address())
// 	if err != nil {
// 		return nil, err
// 	}
// 	account, err = account.Remove(0, tx.Nonce)
// 	if err != nil {
// 		return nil, err
// 	}
// 	if err := tx.vm.putAccount(db, account); err != nil {
// 		return nil, err
// 	}

// 	// If this proposal is committed, create the new blockchain using the chain manager
// 	onAccept := func() {
// 		chainParams := chains.ChainParameters{
// 			ID:          tx.ID(),
// 			GenesisData: tx.GenesisData,
// 			VMAlias:     tx.VMID.String(),
// 		}
// 		for _, fxID := range tx.FxIDs {
// 			chainParams.FxAliases = append(chainParams.FxAliases, fxID.String())
// 		}
// 		// TODO: Not sure how else to make this not nil pointer error during tests
// 		if tx.vm.ChainManager != nil {
// 			tx.vm.ChainManager.CreateChain(chainParams)
// 		}
// 	}

// 	return onAccept, nil
// }

// func (vm *VM) newExportTx(nonce uint64, genesisData []byte, vmID ids.ID, fxIDs []ids.ID, chainName string, networkID uint32, key *crypto.PrivateKeySECP256K1R) (*ExportTx, error) {
// 	tx := &CreateChainTx{
// 		UnsignedCreateChainTx: UnsignedCreateChainTx{
// 			NetworkID:   networkID,
// 			Nonce:       nonce,
// 			GenesisData: genesisData,
// 			VMID:        vmID,
// 			FxIDs:       fxIDs,
// 			ChainName:   chainName,
// 		},
// 	}

// 	unsignedIntf := interface{}(&tx.UnsignedCreateChainTx)
// 	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // Byte repr. of unsigned transaction
// 	if err != nil {
// 		return nil, err
// 	}

// 	sig, err := key.Sign(unsignedBytes)
// 	if err != nil {
// 		return nil, err
// 	}
// 	copy(tx.Sig[:], sig)

// 	return tx, tx.initialize(vm)
// }
