// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type TimedTx interface {
	ID() ids.ID
	StartTime() time.Time
	EndTime() time.Time
	Weight() uint64
	Bytes() []byte
}

// UnsignedTx is an unsigned transaction
type UnsignedTx interface {
	// TODO: Remove this initialization pattern from both the platformvm and the
	// avm.
	snow.ContextInitializable

	Initialize(unsignedBytes, signedBytes []byte)
	ID() ids.ID
	UnsignedBytes() []byte
	Bytes() []byte

	// InputIDs returns the set of inputs this transaction consumes
	InputIDs() ids.Set

	// Attempts to verify this transaction without any provided state.
	SyntacticVerify(ctx *snow.Context) error

	// Attempts to verify this transaction with the provided state.
	SemanticVerify(vm *VM, parentState MutableState, stx *Tx) error
}

// UnsignedDecisionTx is an unsigned operation that can be immediately decided
type UnsignedDecisionTx interface {
	UnsignedTx

	// Execute this transaction with the provided state.
	Execute(vm *VM, vs VersionedState, stx *Tx) (
		onAcceptFunc func() error,
		err error,
	)

	// To maintain consistency with the Atomic txs
	InputUTXOs() ids.Set

	// AtomicOperations provides the requests to be written to shared memory.
	AtomicOperations() (ids.ID, *atomic.Requests, error)
}

// UnsignedProposalTx is an unsigned operation that can be proposed
type UnsignedProposalTx interface {
	UnsignedTx

	// Attempts to verify this transaction with the provided state.
	Execute(vm *VM, state MutableState, stx *Tx) (
		onCommitState VersionedState,
		onAbortState VersionedState,
		err error,
	)
	InitiallyPrefersCommit(vm *VM) bool
}

// UnsignedAtomicTx is an unsigned operation that can be atomically accepted
type UnsignedAtomicTx interface {
	UnsignedDecisionTx

	// Execute this transaction with the provided state.
	AtomicExecute(vm *VM, parentState MutableState, stx *Tx) (VersionedState, error)

	// Accept this transaction with the additionally provided state transitions.
	AtomicAccept(ctx *snow.Context, batch database.Batch) error
}

// Tx is a signed transaction
type Tx struct {
	// The body of this transaction
	UnsignedTx `serialize:"true" json:"unsignedTx"`

	// The credentials of this transaction
	Creds []verify.Verifiable `serialize:"true" json:"credentials"`
}

// Sign this transaction with the provided signers
func (tx *Tx) Sign(c codec.Manager, signers [][]*crypto.PrivateKeySECP256K1R) error {
	unsignedBytes, err := c.Marshal(CodecVersion, &tx.UnsignedTx)
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedTx: %w", err)
	}

	// Attach credentials
	hash := hashing.ComputeHash256(unsignedBytes)
	for _, keys := range signers {
		cred := &secp256k1fx.Credential{
			Sigs: make([][crypto.SECP256K1RSigLen]byte, len(keys)),
		}
		for i, key := range keys {
			sig, err := key.SignHash(hash) // Sign hash
			if err != nil {
				return fmt.Errorf("problem generating credential: %w", err)
			}
			copy(cred.Sigs[i][:], sig)
		}
		tx.Creds = append(tx.Creds, cred) // Attach credential
	}

	signedBytes, err := c.Marshal(CodecVersion, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal ProposalTx: %w", err)
	}
	tx.Initialize(unsignedBytes, signedBytes)
	return nil
}
