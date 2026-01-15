// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/extension"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ atomic.Visitor = (*semanticVerifier)(nil)

var (
	ErrAssetIDMismatch            = errors.New("asset IDs in the input don't match the utxo")
	ErrConflictingAtomicInputs    = errors.New("invalid block due to conflicting atomic inputs")
	errFailedToFetchImportUTXOs   = errors.New("failed to fetch import UTXOs")
	errFailedToUnmarshalUTXO      = errors.New("failed to unmarshal UTXO")
	errRejectedParent             = errors.New("rejected parent")
	errIncorrectNumCredentials    = errors.New("incorrect number of credentials")
	errIncorrectNumSignatures     = errors.New("incorrect number of signatures")
	errPublicKeySignatureMismatch = errors.New("signature doesn't match public key")
)

type BlockFetcher interface {
	// GetExtendedBlock returns the ExtendedBlock for the given ID or an error if the block is not found
	GetExtendedBlock(context.Context, ids.ID) (extension.ExtendedBlock, error)
	// LastAcceptedExtendedBlock returns the last accepted VM block
	LastAcceptedExtendedBlock() extension.ExtendedBlock
}

type VerifierBackend struct {
	Ctx          *snow.Context
	Fx           *secp256k1fx.Fx
	Rules        extras.Rules
	Bootstrapped bool
	BlockFetcher BlockFetcher
	SecpCache    *secp256k1.RecoverCache
}

func NewVerifierBackend(vm *VM, rules extras.Rules) *VerifierBackend {
	return &VerifierBackend{
		Ctx:          vm.Ctx,
		Fx:           &vm.Fx,
		Rules:        rules,
		Bootstrapped: vm.bootstrapped.Get(),
		BlockFetcher: vm,
		SecpCache:    vm.SecpCache,
	}
}

// SemanticVerify checks the semantic validity of atomic transactions.
func (b *VerifierBackend) SemanticVerify(tx *atomic.Tx, parent extension.ExtendedBlock, baseFee *big.Int) error {
	return tx.UnsignedAtomicTx.Visit(&semanticVerifier{
		backend: b,
		tx:      tx,
		parent:  parent,
		baseFee: baseFee,
	})
}

// semanticVerifier is a visitor that checks the semantic validity of atomic transactions.
type semanticVerifier struct {
	backend *VerifierBackend
	tx      *atomic.Tx
	parent  extension.ExtendedBlock
	baseFee *big.Int
}

// ImportTx verifies this transaction is valid.
func (s *semanticVerifier) ImportTx(utx *atomic.UnsignedImportTx) error {
	backend := s.backend
	ctx := backend.Ctx
	rules := backend.Rules
	stx := s.tx
	if err := utx.Verify(ctx, rules); err != nil {
		return err
	}

	// Check the transaction consumes and produces the right amounts
	fc := avax.NewFlowChecker()
	switch {
	// Apply dynamic fees to import transactions as of Apricot Phase 3
	case rules.IsApricotPhase3:
		gasUsed, err := stx.GasUsed(rules.IsApricotPhase5)
		if err != nil {
			return err
		}
		txFee, err := atomic.CalculateDynamicFee(gasUsed, s.baseFee)
		if err != nil {
			return err
		}
		fc.Produce(ctx.AVAXAssetID, txFee)

	// Apply fees to import transactions as of Apricot Phase 2
	case rules.IsApricotPhase2:
		fc.Produce(ctx.AVAXAssetID, ap0.AtomicTxFee)
	}
	for _, out := range utx.Outs {
		fc.Produce(out.AssetID, out.Amount)
	}
	for _, in := range utx.ImportedInputs {
		fc.Consume(in.AssetID(), in.Input().Amount())
	}

	if err := fc.Verify(); err != nil {
		return fmt.Errorf("import tx flow check failed due to: %w", err)
	}

	if len(stx.Creds) != len(utx.ImportedInputs) {
		return fmt.Errorf("%w: (%d vs. %d)", errIncorrectNumCredentials, len(utx.ImportedInputs), len(stx.Creds))
	}

	if !backend.Bootstrapped {
		// Allow for force committing during bootstrapping
		return nil
	}

	utxoIDs := make([][]byte, len(utx.ImportedInputs))
	for i, in := range utx.ImportedInputs {
		inputID := in.UTXOID.InputID()
		utxoIDs[i] = inputID[:]
	}
	// allUTXOBytes is guaranteed to be the same length as utxoIDs
	allUTXOBytes, err := ctx.SharedMemory.Get(utx.SourceChain, utxoIDs)
	if err != nil {
		return fmt.Errorf("%w from %s due to: %w", errFailedToFetchImportUTXOs, utx.SourceChain, err)
	}

	for i, in := range utx.ImportedInputs {
		utxoBytes := allUTXOBytes[i]

		utxo := &avax.UTXO{}
		if _, err := atomic.Codec.Unmarshal(utxoBytes, utxo); err != nil {
			return fmt.Errorf("%w: %w", errFailedToUnmarshalUTXO, err)
		}

		cred := stx.Creds[i]

		utxoAssetID := utxo.AssetID()
		inAssetID := in.AssetID()
		if utxoAssetID != inAssetID {
			return ErrAssetIDMismatch
		}

		if err := backend.Fx.VerifyTransfer(utx, in.In, cred, utxo.Out); err != nil {
			return fmt.Errorf("import tx transfer failed verification: %w", err)
		}
	}

	return conflicts(backend, utx.InputUTXOs(), s.parent)
}

// conflicts returns an error if [inputs] conflicts with any of the atomic inputs contained in [ancestor]
// or any of its ancestor blocks going back to the last accepted block in its ancestry. If [ancestor] is
// accepted, then nil will be returned immediately.
// If the ancestry of [ancestor] cannot be fetched, then [errRejectedParent] may be returned.
func conflicts(backend *VerifierBackend, inputs set.Set[ids.ID], ancestor extension.ExtendedBlock) error {
	fetcher := backend.BlockFetcher
	lastAcceptedBlock := fetcher.LastAcceptedExtendedBlock()
	lastAcceptedHeight := lastAcceptedBlock.Height()
	for ancestor.Height() > lastAcceptedHeight {
		ancestorExtIntf := ancestor.GetBlockExtension()
		ancestorExt, ok := ancestorExtIntf.(atomic.AtomicBlockContext)
		if !ok {
			return fmt.Errorf("expected block extension to be AtomicBlockContext but got %T", ancestorExtIntf)
		}
		// If any of the atomic transactions in the ancestor conflict with [inputs]
		// return an error.
		for _, atomicTx := range ancestorExt.AtomicTxs() {
			if inputs.Overlaps(atomicTx.InputUTXOs()) {
				return ErrConflictingAtomicInputs
			}
		}

		// Move up the chain.
		nextAncestorID := ancestor.Parent()
		// If the ancestor is unknown, then the parent failed
		// verification when it was called.
		// If the ancestor is rejected, then this block shouldn't be
		// inserted into the canonical chain because the parent is
		// will be missing.
		// If the ancestor is processing, then the block may have
		// been verified.
		nextAncestor, err := fetcher.GetExtendedBlock(context.TODO(), nextAncestorID)
		if err != nil {
			return errRejectedParent
		}
		ancestor = nextAncestor
	}

	return nil
}

// ExportTx verifies this transaction is valid.
func (s *semanticVerifier) ExportTx(utx *atomic.UnsignedExportTx) error {
	backend := s.backend
	ctx := backend.Ctx
	rules := backend.Rules
	stx := s.tx
	if err := utx.Verify(ctx, rules); err != nil {
		return err
	}

	// Check the transaction consumes and produces the right amounts
	fc := avax.NewFlowChecker()
	switch {
	// Apply dynamic fees to export transactions as of Apricot Phase 3
	case rules.IsApricotPhase3:
		gasUsed, err := stx.GasUsed(rules.IsApricotPhase5)
		if err != nil {
			return err
		}
		txFee, err := atomic.CalculateDynamicFee(gasUsed, s.baseFee)
		if err != nil {
			return err
		}
		fc.Produce(ctx.AVAXAssetID, txFee)
	// Apply fees to export transactions before Apricot Phase 3
	default:
		fc.Produce(ctx.AVAXAssetID, ap0.AtomicTxFee)
	}
	for _, out := range utx.ExportedOutputs {
		fc.Produce(out.AssetID(), out.Output().Amount())
	}
	for _, in := range utx.Ins {
		fc.Consume(in.AssetID, in.Amount)
	}

	if err := fc.Verify(); err != nil {
		return fmt.Errorf("export tx flow check failed due to: %w", err)
	}

	if len(utx.Ins) != len(stx.Creds) {
		return fmt.Errorf("export tx contained %w want %d got %d", errIncorrectNumCredentials, len(utx.Ins), len(stx.Creds))
	}

	for i, input := range utx.Ins {
		cred, ok := stx.Creds[i].(*secp256k1fx.Credential)
		if !ok {
			return fmt.Errorf("expected *secp256k1fx.Credential but got %T", cred)
		}
		if err := cred.Verify(); err != nil {
			return err
		}

		if len(cred.Sigs) != 1 {
			return fmt.Errorf("%w want 1 signature for EVM Input Credential, but got %d", errIncorrectNumSignatures, len(cred.Sigs))
		}
		pubKey, err := s.backend.SecpCache.RecoverPublicKey(utx.Bytes(), cred.Sigs[0][:])
		if err != nil {
			return err
		}
		if input.Address != pubKey.EthAddress() {
			return errPublicKeySignatureMismatch
		}
	}

	return nil
}
