// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"
	"github.com/holiman/uint256"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap5"

	avalancheutils "github.com/ava-labs/avalanchego/utils"
)

var (
	_                           UnsignedAtomicTx       = (*UnsignedExportTx)(nil)
	_                           secp256k1fx.UnsignedTx = (*UnsignedExportTx)(nil)
	ErrExportNonAVAXInputBanff                         = errors.New("export input cannot contain non-AVAX in Banff")
	ErrExportNonAVAXOutputBanff                        = errors.New("export output cannot contain non-AVAX in Banff")
	ErrNoExportOutputs                                 = errors.New("tx has no export outputs")
	errOverflowExport                                  = errors.New("overflow when computing export amount + txFee")
	errInsufficientFunds                               = errors.New("insufficient funds")
	errInvalidNonce                                    = errors.New("invalid nonce")
)

// UnsignedExportTx is an unsigned ExportTx
type UnsignedExportTx struct {
	Metadata
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
	rules extras.Rules,
) error {
	switch {
	case utx == nil:
		return ErrNilTx
	case len(utx.ExportedOutputs) == 0:
		return ErrNoExportOutputs
	case utx.NetworkID != ctx.NetworkID:
		return ErrWrongNetworkID
	case ctx.ChainID != utx.BlockchainID:
		return ErrWrongChainID
	}

	// Make sure that the tx has a valid peer chain ID
	if rules.IsApricotPhase5 {
		// Note that SameSubnet verifies that [tx.DestinationChain] isn't this
		// chain's ID
		if err := verify.SameSubnet(context.TODO(), ctx, utx.DestinationChain); err != nil {
			return ErrWrongChainID
		}
	} else {
		if utx.DestinationChain != ctx.XChainID {
			return ErrWrongChainID
		}
	}

	for _, in := range utx.Ins {
		if err := in.Verify(); err != nil {
			return err
		}
		if rules.IsBanff && in.AssetID != ctx.AVAXAssetID {
			return ErrExportNonAVAXInputBanff
		}
	}

	for _, out := range utx.ExportedOutputs {
		if err := out.Verify(); err != nil {
			return err
		}
		assetID := out.AssetID()
		if assetID != ctx.AVAXAssetID && utx.DestinationChain == constants.PlatformChainID {
			return ErrWrongChainID
		}
		if rules.IsBanff && assetID != ctx.AVAXAssetID {
			return ErrExportNonAVAXOutputBanff
		}
	}
	if !avax.IsSortedTransferableOutputs(utx.ExportedOutputs, Codec) {
		return ErrOutputsNotSorted
	}
	if rules.IsApricotPhase1 && !avalancheutils.IsSortedAndUnique(utx.Ins) {
		return ErrInputsNotSortedUnique
	}

	return nil
}

func (utx *UnsignedExportTx) GasUsed(fixedFee bool) (uint64, error) {
	byteCost := calcBytesCost(len(utx.Bytes()))
	numSigs := uint64(len(utx.Ins))
	sigCost, err := math.Mul(numSigs, secp256k1fx.CostPerSignature)
	if err != nil {
		return 0, err
	}
	cost, err := math.Add(byteCost, sigCost)
	if err != nil {
		return 0, err
	}
	if fixedFee {
		cost, err = math.Add(cost, ap5.AtomicTxIntrinsicGas)
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
			spent, err = math.Add(spent, out.Output().Amount())
			if err != nil {
				return 0, err
			}
		}
	}
	for _, in := range utx.Ins {
		if in.AssetID == assetID {
			input, err = math.Add(input, in.Amount)
			if err != nil {
				return 0, err
			}
		}
	}

	return math.Sub(input, spent)
}

func (utx *UnsignedExportTx) Visit(v Visitor) error { return v.ExportTx(utx) }

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

		utxoBytes, err := Codec.Marshal(CodecVersion, utxo)
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

// NewExportTx returns a new ExportTx
func NewExportTx(
	ctx *snow.Context,
	rules extras.Rules,
	state StateDB,
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
	if assetID != ctx.AVAXAssetID {
		ins, signers, err = getSpendableFunds(ctx, state, keys, assetID, amount)
		if err != nil {
			return nil, fmt.Errorf("couldn't generate tx inputs/signers: %w", err)
		}
	} else {
		avaxNeeded = amount
	}

	switch {
	case rules.IsApricotPhase3:
		utx := &UnsignedExportTx{
			NetworkID:        ctx.NetworkID,
			BlockchainID:     ctx.ChainID,
			DestinationChain: chainID,
			Ins:              ins,
			ExportedOutputs:  outs,
		}
		tx := &Tx{UnsignedAtomicTx: utx}
		if err := tx.Sign(Codec, nil); err != nil {
			return nil, err
		}

		var cost uint64
		cost, err = tx.GasUsed(rules.IsApricotPhase5)
		if err != nil {
			return nil, err
		}

		avaxIns, avaxSigners, err = getSpendableAVAXWithFee(ctx, state, keys, avaxNeeded, cost, baseFee)
	default:
		var newAvaxNeeded uint64
		newAvaxNeeded, err = math.Add(avaxNeeded, ap0.AtomicTxFee)
		if err != nil {
			return nil, errOverflowExport
		}
		avaxIns, avaxSigners, err = getSpendableFunds(ctx, state, keys, ctx.AVAXAssetID, newAvaxNeeded)
	}
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/signers: %w", err)
	}
	ins = append(ins, avaxIns...)
	signers = append(signers, avaxSigners...)

	avax.SortTransferableOutputs(outs, Codec)
	SortEVMInputsAndSigners(ins, signers)

	// Create the transaction
	utx := &UnsignedExportTx{
		NetworkID:        ctx.NetworkID,
		BlockchainID:     ctx.ChainID,
		DestinationChain: chainID,
		Ins:              ins,
		ExportedOutputs:  outs,
	}
	tx := &Tx{UnsignedAtomicTx: utx}
	if err := tx.Sign(Codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(ctx, rules)
}

// EVMStateTransfer executes the state update from the atomic export transaction
func (utx *UnsignedExportTx) EVMStateTransfer(ctx *snow.Context, state StateDB) error {
	addrs := map[[20]byte]uint64{}
	for _, from := range utx.Ins {
		if from.AssetID == ctx.AVAXAssetID {
			log.Debug("export_tx", "dest", utx.DestinationChain, "addr", from.Address, "amount", from.Amount, "assetID", "AVAX")
			// We multiply the input amount by x2cRate to convert AVAX back to the appropriate
			// denomination before export.
			amount := new(uint256.Int).Mul(
				uint256.NewInt(from.Amount),
				uint256.NewInt(X2CRate.Uint64()),
			)
			if state.GetBalance(from.Address).Cmp(amount) < 0 {
				return errInsufficientFunds
			}
			state.SubBalance(from.Address, amount)
		} else {
			log.Debug("export_tx", "dest", utx.DestinationChain, "addr", from.Address, "amount", from.Amount, "assetID", from.AssetID)
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

// getSpendableFunds returns a list of EVMInputs and keys (in corresponding
// order) to total [amount] of [assetID] owned by [keys].
// Note: we return [][]*secp256k1.PrivateKey even though each input
// corresponds to a single key, so that the signers can be passed in to
// [tx.Sign] which supports multiple keys on a single input.
func getSpendableFunds(
	ctx *snow.Context,
	state StateDB,
	keys []*secp256k1.PrivateKey,
	assetID ids.ID,
	amount uint64,
) ([]EVMInput, [][]*secp256k1.PrivateKey, error) {
	inputs := []EVMInput{}
	signers := [][]*secp256k1.PrivateKey{}
	// Note: we assume that each key in [keys] is unique, so that iterating over
	// the keys will not produce duplicated nonces in the returned EVMInput slice.
	for _, key := range keys {
		if amount == 0 {
			break
		}
		addr := key.EthAddress()
		var balance uint64
		if assetID == ctx.AVAXAssetID {
			// If the asset is AVAX, we divide by the x2cRate to convert back to the correct
			// denomination of AVAX that can be exported.
			balance = new(uint256.Int).Div(state.GetBalance(addr), X2CRate).Uint64()
		} else {
			balance = state.GetBalanceMultiCoin(addr, common.Hash(assetID)).Uint64()
		}
		if balance == 0 {
			continue
		}
		if amount < balance {
			balance = amount
		}
		nonce := state.GetNonce(addr)

		inputs = append(inputs, EVMInput{
			Address: addr,
			Amount:  balance,
			AssetID: assetID,
			Nonce:   nonce,
		})
		signers = append(signers, []*secp256k1.PrivateKey{key})
		amount -= balance
	}

	if amount > 0 {
		return nil, nil, errInsufficientFunds
	}

	return inputs, signers, nil
}

// getSpendableAVAXWithFee returns a list of EVMInputs and keys (in corresponding
// order) to total [amount] + [fee] of [AVAX] owned by [keys].
// This function accounts for the added cost of the additional inputs needed to
// create the transaction and makes sure to skip any keys with a balance that is
// insufficient to cover the additional fee.
// Note: we return [][]*secp256k1.PrivateKey even though each input
// corresponds to a single key, so that the signers can be passed in to
// [tx.Sign] which supports multiple keys on a single input.
func getSpendableAVAXWithFee(
	ctx *snow.Context,
	state StateDB,
	keys []*secp256k1.PrivateKey,
	amount uint64,
	cost uint64,
	baseFee *big.Int,
) ([]EVMInput, [][]*secp256k1.PrivateKey, error) {
	initialFee, err := CalculateDynamicFee(cost, baseFee)
	if err != nil {
		return nil, nil, err
	}

	newAmount, err := math.Add(amount, initialFee)
	if err != nil {
		return nil, nil, err
	}
	amount = newAmount

	inputs := []EVMInput{}
	signers := [][]*secp256k1.PrivateKey{}
	// Note: we assume that each key in [keys] is unique, so that iterating over
	// the keys will not produce duplicated nonces in the returned EVMInput slice.
	for _, key := range keys {
		if amount == 0 {
			break
		}

		prevFee, err := CalculateDynamicFee(cost, baseFee)
		if err != nil {
			return nil, nil, err
		}

		newCost := cost + EVMInputGas
		newFee, err := CalculateDynamicFee(newCost, baseFee)
		if err != nil {
			return nil, nil, err
		}

		additionalFee := newFee - prevFee

		addr := key.EthAddress()
		// Since the asset is AVAX, we divide by the x2cRate to convert back to
		// the correct denomination of AVAX that can be exported.
		balance := new(uint256.Int).Div(state.GetBalance(addr), X2CRate).Uint64()
		// If the balance for [addr] is insufficient to cover the additional cost
		// of adding an input to the transaction, skip adding the input altogether
		if balance <= additionalFee {
			continue
		}

		// Update the cost for the next iteration
		cost = newCost

		newAmount, err := math.Add(amount, additionalFee)
		if err != nil {
			return nil, nil, err
		}
		amount = newAmount

		// Use the entire [balance] as an input, but if the required [amount]
		// is less than the balance, update the [inputAmount] to spend the
		// minimum amount to finish the transaction.
		inputAmount := balance
		if amount < balance {
			inputAmount = amount
		}
		nonce := state.GetNonce(addr)

		inputs = append(inputs, EVMInput{
			Address: addr,
			Amount:  inputAmount,
			AssetID: ctx.AVAXAssetID,
			Nonce:   nonce,
		})
		signers = append(signers, []*secp256k1.PrivateKey{key})
		amount -= inputAmount
	}

	if amount > 0 {
		return nil, nil, errInsufficientFunds
	}

	return inputs, signers, nil
}
