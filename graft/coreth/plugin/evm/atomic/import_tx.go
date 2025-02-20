// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap5"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

var (
	_                           UnsignedAtomicTx       = &UnsignedImportTx{}
	_                           secp256k1fx.UnsignedTx = &UnsignedImportTx{}
	ErrImportNonAVAXInputBanff                         = errors.New("import input cannot contain non-AVAX in Banff")
	ErrImportNonAVAXOutputBanff                        = errors.New("import output cannot contain non-AVAX in Banff")
	ErrNoImportInputs                                  = errors.New("tx has no imported inputs")
	ErrConflictingAtomicInputs                         = errors.New("invalid block due to conflicting atomic inputs")
	ErrWrongChainID                                    = errors.New("tx has wrong chain ID")
	ErrNoEVMOutputs                                    = errors.New("tx has no EVM outputs")
	ErrInputsNotSortedUnique                           = errors.New("inputs not sorted and unique")
	ErrOutputsNotSortedUnique                          = errors.New("outputs not sorted and unique")
	ErrOutputsNotSorted                                = errors.New("tx outputs not sorted")
	ErrAssetIDMismatch                                 = errors.New("asset IDs in the input don't match the utxo")
	errNilBaseFeeApricotPhase3                         = errors.New("nil base fee is invalid after apricotPhase3")
	errInsufficientFundsForFee                         = errors.New("insufficient AVAX funds to pay transaction fee")
	errRejectedParent                                  = errors.New("rejected parent")
)

// UnsignedImportTx is an unsigned ImportTx
type UnsignedImportTx struct {
	Metadata
	// ID of the network on which this tx was issued
	NetworkID uint32 `serialize:"true" json:"networkID"`
	// ID of this blockchain.
	BlockchainID ids.ID `serialize:"true" json:"blockchainID"`
	// Which chain to consume the funds from
	SourceChain ids.ID `serialize:"true" json:"sourceChain"`
	// Inputs that consume UTXOs produced on the chain
	ImportedInputs []*avax.TransferableInput `serialize:"true" json:"importedInputs"`
	// Outputs
	Outs []EVMOutput `serialize:"true" json:"outputs"`
}

// InputUTXOs returns the UTXOIDs of the imported funds
func (utx *UnsignedImportTx) InputUTXOs() set.Set[ids.ID] {
	set := set.NewSet[ids.ID](len(utx.ImportedInputs))
	for _, in := range utx.ImportedInputs {
		set.Add(in.InputID())
	}
	return set
}

// Verify this transaction is well-formed
func (utx *UnsignedImportTx) Verify(
	ctx *snow.Context,
	rules params.Rules,
) error {
	switch {
	case utx == nil:
		return ErrNilTx
	case len(utx.ImportedInputs) == 0:
		return ErrNoImportInputs
	case utx.NetworkID != ctx.NetworkID:
		return ErrWrongNetworkID
	case ctx.ChainID != utx.BlockchainID:
		return ErrWrongChainID
	case rules.IsApricotPhase3 && len(utx.Outs) == 0:
		return ErrNoEVMOutputs
	}

	// Make sure that the tx has a valid peer chain ID
	if rules.IsApricotPhase5 {
		// Note that SameSubnet verifies that [tx.SourceChain] isn't this
		// chain's ID
		if err := verify.SameSubnet(context.TODO(), ctx, utx.SourceChain); err != nil {
			return ErrWrongChainID
		}
	} else {
		if utx.SourceChain != ctx.XChainID {
			return ErrWrongChainID
		}
	}

	for _, out := range utx.Outs {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("EVM Output failed verification: %w", err)
		}
		if rules.IsBanff && out.AssetID != ctx.AVAXAssetID {
			return ErrImportNonAVAXOutputBanff
		}
	}

	for _, in := range utx.ImportedInputs {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("atomic input failed verification: %w", err)
		}
		if rules.IsBanff && in.AssetID() != ctx.AVAXAssetID {
			return ErrImportNonAVAXInputBanff
		}
	}
	if !utils.IsSortedAndUnique(utx.ImportedInputs) {
		return ErrInputsNotSortedUnique
	}

	if rules.IsApricotPhase2 {
		if !utils.IsSortedAndUnique(utx.Outs) {
			return ErrOutputsNotSortedUnique
		}
	} else if rules.IsApricotPhase1 {
		if !slices.IsSortedFunc(utx.Outs, EVMOutput.Compare) {
			return ErrOutputsNotSorted
		}
	}

	return nil
}

func (utx *UnsignedImportTx) GasUsed(fixedFee bool) (uint64, error) {
	var (
		cost = calcBytesCost(len(utx.Bytes()))
		err  error
	)
	for _, in := range utx.ImportedInputs {
		inCost, err := in.In.Cost()
		if err != nil {
			return 0, err
		}
		cost, err = math.Add64(cost, inCost)
		if err != nil {
			return 0, err
		}
	}
	if fixedFee {
		cost, err = math.Add64(cost, ap5.AtomicTxIntrinsicGas)
		if err != nil {
			return 0, err
		}
	}
	return cost, nil
}

// Amount of [assetID] burned by this transaction
func (utx *UnsignedImportTx) Burned(assetID ids.ID) (uint64, error) {
	var (
		spent uint64
		input uint64
		err   error
	)
	for _, out := range utx.Outs {
		if out.AssetID == assetID {
			spent, err = math.Add64(spent, out.Amount)
			if err != nil {
				return 0, err
			}
		}
	}
	for _, in := range utx.ImportedInputs {
		if in.AssetID() == assetID {
			input, err = math.Add64(input, in.Input().Amount())
			if err != nil {
				return 0, err
			}
		}
	}

	return math.Sub(input, spent)
}

// SemanticVerify this transaction is valid.
func (utx *UnsignedImportTx) SemanticVerify(
	backend *Backend,
	stx *Tx,
	parent AtomicBlockContext,
	baseFee *big.Int,
) error {
	ctx := backend.Ctx
	rules := backend.Rules
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
		txFee, err := CalculateDynamicFee(gasUsed, baseFee)
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
		return fmt.Errorf("import tx contained mismatched number of inputs/credentials (%d vs. %d)", len(utx.ImportedInputs), len(stx.Creds))
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
		return fmt.Errorf("failed to fetch import UTXOs from %s due to: %w", utx.SourceChain, err)
	}

	for i, in := range utx.ImportedInputs {
		utxoBytes := allUTXOBytes[i]

		utxo := &avax.UTXO{}
		if _, err := Codec.Unmarshal(utxoBytes, utxo); err != nil {
			return fmt.Errorf("failed to unmarshal UTXO: %w", err)
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

	return conflicts(backend, utx.InputUTXOs(), parent)
}

// AtomicOps returns imported inputs spent on this transaction
// We spend imported UTXOs here rather than in semanticVerify because
// we don't want to remove an imported UTXO in semanticVerify
// only to have the transaction not be Accepted. This would be inconsistent.
// Recall that imported UTXOs are not kept in a versionDB.
func (utx *UnsignedImportTx) AtomicOps() (ids.ID, *atomic.Requests, error) {
	utxoIDs := make([][]byte, len(utx.ImportedInputs))
	for i, in := range utx.ImportedInputs {
		inputID := in.InputID()
		utxoIDs[i] = inputID[:]
	}
	return utx.SourceChain, &atomic.Requests{RemoveRequests: utxoIDs}, nil
}

// NewImportTx returns a new ImportTx
func NewImportTx(
	ctx *snow.Context,
	rules params.Rules,
	time uint64,
	chainID ids.ID, // chain to import from
	to common.Address, // Address of recipient
	baseFee *big.Int, // fee to use post-AP3
	kc *secp256k1fx.Keychain, // Keychain to use for signing the atomic UTXOs
	atomicUTXOs []*avax.UTXO, // UTXOs to spend
) (*Tx, error) {
	importedInputs := []*avax.TransferableInput{}
	signers := [][]*secp256k1.PrivateKey{}

	importedAmount := make(map[ids.ID]uint64)
	for _, utxo := range atomicUTXOs {
		inputIntf, utxoSigners, err := kc.Spend(utxo.Out, time)
		if err != nil {
			continue
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			continue
		}
		aid := utxo.AssetID()
		importedAmount[aid], err = math.Add64(importedAmount[aid], input.Amount())
		if err != nil {
			return nil, err
		}
		importedInputs = append(importedInputs, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In:     input,
		})
		signers = append(signers, utxoSigners)
	}
	avax.SortTransferableInputsWithSigners(importedInputs, signers)
	importedAVAXAmount := importedAmount[ctx.AVAXAssetID]

	outs := make([]EVMOutput, 0, len(importedAmount))
	// This will create unique outputs (in the context of sorting)
	// since each output will have a unique assetID
	for assetID, amount := range importedAmount {
		// Skip the AVAX amount since it is included separately to account for
		// the fee
		if assetID == ctx.AVAXAssetID || amount == 0 {
			continue
		}
		outs = append(outs, EVMOutput{
			Address: to,
			Amount:  amount,
			AssetID: assetID,
		})
	}

	var (
		txFeeWithoutChange uint64
		txFeeWithChange    uint64
	)
	switch {
	case rules.IsApricotPhase3:
		if baseFee == nil {
			return nil, errNilBaseFeeApricotPhase3
		}
		utx := &UnsignedImportTx{
			NetworkID:      ctx.NetworkID,
			BlockchainID:   ctx.ChainID,
			Outs:           outs,
			ImportedInputs: importedInputs,
			SourceChain:    chainID,
		}
		tx := &Tx{UnsignedAtomicTx: utx}
		if err := tx.Sign(Codec, nil); err != nil {
			return nil, err
		}

		gasUsedWithoutChange, err := tx.GasUsed(rules.IsApricotPhase5)
		if err != nil {
			return nil, err
		}
		gasUsedWithChange := gasUsedWithoutChange + EVMOutputGas

		txFeeWithoutChange, err = CalculateDynamicFee(gasUsedWithoutChange, baseFee)
		if err != nil {
			return nil, err
		}
		txFeeWithChange, err = CalculateDynamicFee(gasUsedWithChange, baseFee)
		if err != nil {
			return nil, err
		}
	case rules.IsApricotPhase2:
		txFeeWithoutChange = ap0.AtomicTxFee
		txFeeWithChange = ap0.AtomicTxFee
	}

	// AVAX output
	if importedAVAXAmount < txFeeWithoutChange { // imported amount goes toward paying tx fee
		return nil, errInsufficientFundsForFee
	}

	if importedAVAXAmount > txFeeWithChange {
		outs = append(outs, EVMOutput{
			Address: to,
			Amount:  importedAVAXAmount - txFeeWithChange,
			AssetID: ctx.AVAXAssetID,
		})
	}

	// If no outputs are produced, return an error.
	// Note: this can happen if there is exactly enough AVAX to pay the
	// transaction fee, but no other funds to be imported.
	if len(outs) == 0 {
		return nil, ErrNoEVMOutputs
	}

	utils.Sort(outs)

	// Create the transaction
	utx := &UnsignedImportTx{
		NetworkID:      ctx.NetworkID,
		BlockchainID:   ctx.ChainID,
		Outs:           outs,
		ImportedInputs: importedInputs,
		SourceChain:    chainID,
	}
	tx := &Tx{UnsignedAtomicTx: utx}
	if err := tx.Sign(Codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(ctx, rules)
}

// EVMStateTransfer performs the state transfer to increase the balances of
// accounts accordingly with the imported EVMOutputs
func (utx *UnsignedImportTx) EVMStateTransfer(ctx *snow.Context, state StateDB) error {
	for _, to := range utx.Outs {
		if to.AssetID == ctx.AVAXAssetID {
			log.Debug("import_tx", "src", utx.SourceChain, "addr", to.Address, "amount", to.Amount, "assetID", "AVAX")
			// If the asset is AVAX, convert the input amount in nAVAX to gWei by
			// multiplying by the x2c rate.
			amount := new(uint256.Int).Mul(uint256.NewInt(to.Amount), X2CRate)
			state.AddBalance(to.Address, amount)
		} else {
			log.Debug("import_tx", "src", utx.SourceChain, "addr", to.Address, "amount", to.Amount, "assetID", to.AssetID)
			amount := new(big.Int).SetUint64(to.Amount)
			state.AddBalanceMultiCoin(to.Address, common.Hash(to.AssetID), amount)
		}
	}
	return nil
}

// conflicts returns an error if [inputs] conflicts with any of the atomic inputs contained in [ancestor]
// or any of its ancestor blocks going back to the last accepted block in its ancestry. If [ancestor] is
// accepted, then nil will be returned immediately.
// If the ancestry of [ancestor] cannot be fetched, then [errRejectedParent] may be returned.
func conflicts(backend *Backend, inputs set.Set[ids.ID], ancestor AtomicBlockContext) error {
	fetcher := backend.BlockFetcher
	lastAcceptedBlock := fetcher.LastAcceptedBlockInternal()
	lastAcceptedHeight := lastAcceptedBlock.Height()
	for ancestor.Height() > lastAcceptedHeight {
		// If any of the atomic transactions in the ancestor conflict with [inputs]
		// return an error.
		for _, atomicTx := range ancestor.AtomicTxs() {
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
		nextAncestorIntf, err := fetcher.GetBlockInternal(context.TODO(), nextAncestorID)
		if err != nil {
			return errRejectedParent
		}
		nextAncestor, ok := nextAncestorIntf.(AtomicBlockContext)
		if !ok {
			return fmt.Errorf("ancestor block %s had unexpected type %T", nextAncestor.ID(), nextAncestorIntf)
		}
		ancestor = nextAncestor
	}

	return nil
}
