// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

// UnsignedDecisionTx ...
type UnsignedDecisionTx interface {
	initialize(vm *VM, bytes []byte) error
	ID() ids.ID
	// Attempts to verify this transaction with the provided state.
	SemanticVerify(db database.Database, stx *DecisionTx) (
		onAcceptFunc func() error,
		err TxError,
	)
}

// DecisionTx is an operation that can be decided without being proposed
type DecisionTx struct {
	UnsignedDecisionTx `serialize:"true", json:"unsignedDecisionTx"`
	// Credentials that authorize the inputs to be spent
	Credentials []verify.Verifiable `serialize:"true", json:"credentials"`
}

func (vm *VM) signDecisionTx(tx *DecisionTx, signers [][]*crypto.PrivateKeySECP256K1R) error {
	unsignedBytes, err := vm.codec.Marshal(tx.UnsignedDecisionTx)
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedDecisionTx: %w", err)
	}

	// Attach credentials
	hash := hashing.ComputeHash256(unsignedBytes)
	tx.Credentials = make([]verify.Verifiable, len(signers))
	for i, credKeys := range signers {
		cred := &secp256k1fx.Credential{
			Sigs: make([][crypto.SECP256K1RSigLen]byte, len(credKeys)),
		}
		for j, key := range credKeys {
			sig, err := key.SignHash(hash) // Sign hash
			if err != nil {
				return fmt.Errorf("problem generating credential: %w", err)
			}
			copy(cred.Sigs[j][:], sig)
		}
		tx.Credentials[i] = cred // Attach credential
	}

	txBytes, err := vm.codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal ProposalTx: %w", err)
	}
	return tx.initialize(vm, txBytes)
}
