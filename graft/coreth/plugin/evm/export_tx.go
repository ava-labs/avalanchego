// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
)

var (
	_                           UnsignedAtomicTx       = &UnsignedExportTx{}
	_                           secp256k1fx.UnsignedTx = &UnsignedExportTx{}
	errExportNonAVAXInputBanff                         = errors.New("export input cannot contain non-AVAX in Banff")
	errExportNonAVAXOutputBanff                        = errors.New("export output cannot contain non-AVAX in Banff")
)

// UnsignedExportTx is an unsigned ExportTx
type UnsignedExportTx struct {
	avax.Metadata
	// ID of the network on which this tx was issued
	NetworkID uint32 `serialize:"true" json:"networkID"`
	// ID of this blockchain.
	BlockchainID ids.ID `serialize:"true" json:"blockchainID"`
	// Which chain to send the funds to
	DestinationChain ids.ID `serialize:"true" json:"destinationChain"`
	// Inputs
	Ins []EVMInput `serialize:"true" json:"inputs"`
	// Outputs that are exported to the chain
	ExportedOutputs []*avax.TransferableOutput `serialize:"true" json:"exportedOutputs"`
}

// InputUTXOs returns a set of all the hash(address:nonce) exporting funds.
func (utx *UnsignedExportTx) InputUTXOs() set.Set[ids.ID] {
	set := set.NewSet[ids.ID](len(utx.Ins))
	for _, in := range utx.Ins {
		// Total populated bytes is exactly 32 bytes.
		// 8 (Nonce) + 4 (Address Length) + 20 (Address)
		var rawID [32]byte
		packer := wrappers.Packer{Bytes: rawID[:]}
		packer.PackLong(in.Nonce)
		packer.PackBytes(in.Address.Bytes())
		set.Add(ids.ID(rawID))
	}
	return set
}

// Verify this transaction is well-formed
func (utx *UnsignedExportTx) Verify(
	ctx *snow.Context,
	rules params.Rules,
) error {
	switch {
	case utx == nil:
		return errNilTx
	case len(utx.ExportedOutputs) == 0:
		return errNoExportOutputs
	case utx.NetworkID != ctx.NetworkID:
		return errWrongNetworkID
	case ctx.ChainID != utx.BlockchainID:
		return errWrongBlockchainID
	}

	// Make sure that the tx has a valid peer chain ID
	if rules.IsApricotPhase5 {
		// Note that SameSubnet verifies that [tx.DestinationChain] isn't this
		// chain's ID
		if err := verify.SameSubnet(context.TODO(), ctx, utx.DestinationChain); err != nil {
			return errWrongChainID
		}
	} else {
		if utx.DestinationChain != ctx.XChainID {
			return errWrongChainID
		}
	}

	for _, in := range utx.Ins {
		if err := in.Verify(); err != nil {
			return err
		}
		if rules.IsBanff && in.AssetID != ctx.AVAXAssetID {
			return errExportNonAVAXInputBanff
		}
	}

	for _, out := range utx.ExportedOutputs {
		if err := out.Verify(); err != nil {
			return err
		}
		assetID := out.AssetID()
		if assetID != ctx.AVAXAssetID && utx.DestinationChain == constants.PlatformChainID {
			return errWrongChainID
		}
		if rules.IsBanff && assetID != ctx.AVAXAssetID {
			return errExportNonAVAXOutputBanff
		}
	}
	if !avax.IsSortedTransferableOutputs(utx.ExportedOutputs, Codec) {
		return errOutputsNotSorted
	}
	if rules.IsApricotPhase1 && !utils.IsSortedAndUnique(utx.Ins) {
		return errInputsNotSortedUnique
	}

	return nil
}

func (utx *UnsignedExportTx) GasUsed(fixedFee bool) (uint64, error) {
	byteCost := calcBytesCost(len(utx.Bytes()))
	numSigs := uint64(len(utx.Ins))
	sigCost, err := math.Mul64(numSigs, secp256k1fx.CostPerSignature)
	if err != nil {
		return 0, err
	}
	cost, err := math.Add64(byteCost, sigCost)
	if err != nil {
		return 0, err
	}
	if fixedFee {
		cost, err = math.Add64(cost, params.AtomicTxBaseCost)
		if err != nil {
			return 0, err
		}
	}

	return cost, nil
}

// Amount of [assetID] burned by this transaction
func (utx *UnsignedExportTx) Burned(assetID ids.ID) (uint64, error) {
	var (
		spent uint64
		input uint64
		err   error
	)
	for _, out := range utx.ExportedOutputs {
		if out.AssetID() == assetID {
			spent, err = math.Add64(spent, out.Output().Amount())
			if err != nil {
				return 0, err
			}
		}
	}
	for _, in := range utx.Ins {
		if in.AssetID == assetID {
			input, err = math.Add64(input, in.Amount)
			if err != nil {
				return 0, err
			}
		}
	}

	return math.Sub(input, spent)
}

// SemanticVerify this transaction is valid.
func (utx *UnsignedExportTx) SemanticVerify(
	vm *VM,
	stx *Tx,
	_ *Block,
	baseFee *big.Int,
	rules params.Rules,
) error {
	if err := utx.Verify(vm.ctx, rules); err != nil {
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
		txFee, err := CalculateDynamicFee(gasUsed, baseFee)
		if err != nil {
			return err
		}
		fc.Produce(vm.ctx.AVAXAssetID, txFee)
	// Apply fees to export transactions before Apricot Phase 3
	default:
		fc.Produce(vm.ctx.AVAXAssetID, params.AvalancheAtomicTxFee)
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
		return fmt.Errorf("export tx contained mismatched number of inputs/credentials (%d vs. %d)", len(utx.Ins), len(stx.Creds))
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
			return fmt.Errorf("expected one signature for EVM Input Credential, but found: %d", len(cred.Sigs))
		}
		pubKey, err := vm.secpFactory.RecoverPublicKey(utx.Bytes(), cred.Sigs[0][:])
		if err != nil {
			return err
		}
		if input.Address != PublicKeyToEthAddress(pubKey) {
			return errPublicKeySignatureMismatch
		}
	}

	return nil
}

// AtomicOps returns the atomic operations for this transaction.
func (utx *UnsignedExportTx) AtomicOps() (ids.ID, *atomic.Requests, error) {
	txID := utx.ID()

	elems := make([]*atomic.Element, len(utx.ExportedOutputs))
	for i, out := range utx.ExportedOutputs {
		utxo := &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(i),
			},
			Asset: avax.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}

		utxoBytes, err := Codec.Marshal(codecVersion, utxo)
		if err != nil {
			return ids.ID{}, nil, err
		}
		utxoID := utxo.InputID()
		elem := &atomic.Element{
			Key:   utxoID[:],
			Value: utxoBytes,
		}
		if out, ok := utxo.Out.(avax.Addressable); ok {
			elem.Traits = out.Addresses()
		}

		elems[i] = elem
	}

	return utx.DestinationChain, &atomic.Requests{PutRequests: elems}, nil
}

// newExportTx returns a new ExportTx
func (vm *VM) newExportTx(
	assetID ids.ID, // AssetID of the tokens to export
	amount uint64, // Amount of tokens to export
	chainID ids.ID, // Chain to send the UTXOs to
	to ids.ShortID, // Address of chain recipient
	baseFee *big.Int, // fee to use post-AP3
	keys []*secp256k1.PrivateKey, // Pay the fee and provide the tokens
) (*Tx, error) {
	outs := []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: assetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     []ids.ShortID{to},
			},
		},
	}}

	var (
		avaxNeeded           uint64 = 0
		ins, avaxIns         []EVMInput
		signers, avaxSigners [][]*secp256k1.PrivateKey
		err                  error
	)

	// consume non-AVAX
	if assetID != vm.ctx.AVAXAssetID {
		ins, signers, err = vm.GetSpendableFunds(keys, assetID, amount)
		if err != nil {
			return nil, fmt.Errorf("couldn't generate tx inputs/signers: %w", err)
		}
	} else {
		avaxNeeded = amount
	}

	rules := vm.currentRules()
	switch {
	case rules.IsApricotPhase3:
		utx := &UnsignedExportTx{
			NetworkID:        vm.ctx.NetworkID,
			BlockchainID:     vm.ctx.ChainID,
			DestinationChain: chainID,
			Ins:              ins,
			ExportedOutputs:  outs,
		}
		tx := &Tx{UnsignedAtomicTx: utx}
		if err := tx.Sign(vm.codec, nil); err != nil {
			return nil, err
		}

		var cost uint64
		cost, err = tx.GasUsed(rules.IsApricotPhase5)
		if err != nil {
			return nil, err
		}

		avaxIns, avaxSigners, err = vm.GetSpendableAVAXWithFee(keys, avaxNeeded, cost, baseFee)
	default:
		var newAvaxNeeded uint64
		newAvaxNeeded, err = math.Add64(avaxNeeded, params.AvalancheAtomicTxFee)
		if err != nil {
			return nil, errOverflowExport
		}
		avaxIns, avaxSigners, err = vm.GetSpendableFunds(keys, vm.ctx.AVAXAssetID, newAvaxNeeded)
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/signers: %w", err)
	}
	ins = append(ins, avaxIns...)
	signers = append(signers, avaxSigners...)

	avax.SortTransferableOutputs(outs, vm.codec)
	SortEVMInputsAndSigners(ins, signers)

	// Create the transaction
	utx := &UnsignedExportTx{
		NetworkID:        vm.ctx.NetworkID,
		BlockchainID:     vm.ctx.ChainID,
		DestinationChain: chainID,
		Ins:              ins,
		ExportedOutputs:  outs,
	}
	tx := &Tx{UnsignedAtomicTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(vm.ctx, vm.currentRules())
}

// EVMStateTransfer executes the state update from the atomic export transaction
func (utx *UnsignedExportTx) EVMStateTransfer(ctx *snow.Context, state *state.StateDB) error {
	addrs := map[[20]byte]uint64{}
	for _, from := range utx.Ins {
		if from.AssetID == ctx.AVAXAssetID {
			log.Debug("crosschain", "dest", utx.DestinationChain, "addr", from.Address, "amount", from.Amount, "assetID", "AVAX")
			// We multiply the input amount by x2cRate to convert AVAX back to the appropriate
			// denomination before export.
			amount := new(big.Int).Mul(
				new(big.Int).SetUint64(from.Amount), x2cRate)
			if state.GetBalance(from.Address).Cmp(amount) < 0 {
				return errInsufficientFunds
			}
			state.SubBalance(from.Address, amount)
		} else {
			log.Debug("crosschain", "dest", utx.DestinationChain, "addr", from.Address, "amount", from.Amount, "assetID", from.AssetID)
			amount := new(big.Int).SetUint64(from.Amount)
			if state.GetBalanceMultiCoin(from.Address, common.Hash(from.AssetID)).Cmp(amount) < 0 {
				return errInsufficientFunds
			}
			state.SubBalanceMultiCoin(from.Address, common.Hash(from.AssetID), amount)
		}
		if state.GetNonce(from.Address) != from.Nonce {
			return errInvalidNonce
		}
		addrs[from.Address] = from.Nonce
	}
	for addr, nonce := range addrs {
		state.SetNonce(addr, nonce+1)
	}
	return nil
}
