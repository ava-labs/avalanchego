// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common"
	"github.com/holiman/uint256"

	// Imported for [atomic.UnsignedImportTx.Burned] comment resolution.
	_ "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

var _ Unsigned = (*Import)(nil)

const (
	// EVMOwnerLocktime marks a UTXO whose owner is an EVM address. Such a
	// UTXO is imported to that address by an [Import] with no credentials.
	// Legacy UTXOs (locktime 0) are owned by ripemd160(sha256(pubkey)), an
	// address nobody holds an EVM key for, so they keep needing a signature.
	//
	// ponytail: locktime doubles as the marker; a dedicated output type is
	// the cleanup once the P-chain wallets ship it.
	EVMOwnerLocktime uint64 = 1
	// MaxUnsignedImportBurn caps the fee an [Import] with no credentials may
	// burn as posted. The block builder reprices it to the base fee anyway
	// ([Tx.RepriceUnsigned]); this only bounds what the mempool admits.
	MaxUnsignedImportBurn nAVAX = 10 * units.MilliAvax
)

// Import is the unsigned component of a transaction that transfers assets from
// either the P-Chain or the X-Chain to the C-Chain. It consumes UTXOs in the
// shared memory between the C-Chain and the source chain and increases balances
// in the C-Chain state.
type Import struct {
	NetworkID      uint32                    `serialize:"true" json:"networkID"`
	BlockchainID   ids.ID                    `serialize:"true" json:"blockchainID"`
	SourceChain    ids.ID                    `serialize:"true" json:"sourceChain"`
	ImportedInputs []*avax.TransferableInput `serialize:"true" json:"importedInputs"`
	Outs           []Output                  `serialize:"true" json:"outputs"`
}

// Output specifies an account on the C-Chain whose balance of the specified
// asset should be increased.
//
// If the AssetID is AVAX, the amount will be scaled up to account for the EVM's
// higher denomination.
type Output struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
	AssetID ids.ID         `serialize:"true" json:"assetID"`
}

// Compare orders [Output] values by [Output.Address] and [Output.AssetID].
func (o Output) Compare(other Output) int {
	if c := o.Address.Cmp(other.Address); c != 0 {
		return c
	}
	return o.AssetID.Compare(other.AssetID)
}

func (i *Import) inputIDs() set.Set[ids.ID] {
	s := set.NewSet[ids.ID](len(i.ImportedInputs))
	for _, in := range i.ImportedInputs {
		s.Add(in.InputID())
	}
	return s
}

// Like [atomic.UnsignedImportTx.Burned], burned will error if the sum of the
// inputs exceeds MaxUint64, even if the total amount burned could be
// represented as a uint64.
//
// Because the total supply of AVAX fits in a uint64, this doesn't matter in
// practice and allows for easier fuzzing.
func (i *Import) burned(avaxAssetID ids.ID) (nAVAX, error) {
	var (
		burned nAVAX
		err    error
	)
	for _, in := range i.ImportedInputs {
		if in.Asset.ID == avaxAssetID {
			burned, err = math.Add(burned, in.In.Amount())
			if err != nil {
				return 0, err
			}
		}
	}
	for _, out := range i.Outs {
		if out.AssetID == avaxAssetID {
			burned, err = math.Sub(burned, out.Amount)
			if err != nil {
				return 0, err
			}
		}
	}
	return burned, nil
}

var errOutputsNotSortedUnique = errors.New("outputs not sorted and unique")

func (i *Import) sanityCheck(ctx *snow.Context) error {
	switch {
	case i.NetworkID != ctx.NetworkID:
		return fmt.Errorf("%w: want %d, got %d", errWrongNetworkID, ctx.NetworkID, i.NetworkID)
	case i.BlockchainID != ctx.ChainID:
		return fmt.Errorf("%w: want %s, got %s", errWrongChainID, ctx.ChainID, i.BlockchainID)
	case i.SourceChain != constants.PlatformChainID && i.SourceChain != ctx.XChainID:
		return fmt.Errorf("%w: want %s or %s, got %s", errNotSameSubnet, constants.PlatformChainID, ctx.XChainID, i.SourceChain)
	case len(i.ImportedInputs) == 0:
		return errNoInputs
	case len(i.Outs) == 0:
		return errNoOutputs
	}

	fc := avax.NewFlowChecker()
	for j, in := range i.ImportedInputs {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("%w (%d): %w", errInvalidInput, j, err)
		}
		if assetID := in.Asset.ID; assetID != ctx.AVAXAssetID {
			return fmt.Errorf("%w (%d): want %s, got %s", errNonAVAXInput, j, ctx.AVAXAssetID, assetID)
		}
		fc.Consume(ctx.AVAXAssetID, in.In.Amount())
	}
	for j, out := range i.Outs {
		if out.Amount == 0 {
			return fmt.Errorf("%w (%d): zero amount", errInvalidOutput, j)
		}
		if out.AssetID != ctx.AVAXAssetID {
			return fmt.Errorf("%w (%d): want %s, got %s", errNonAVAXOutput, j, ctx.AVAXAssetID, out.AssetID)
		}
		fc.Produce(ctx.AVAXAssetID, out.Amount)
	}
	if err := fc.Verify(); err != nil {
		return fmt.Errorf("%w: %w", errFlowCheckFailed, err)
	}

	if !utils.IsSortedAndUnique(i.ImportedInputs) {
		return errInputsNotSortedUnique
	}
	if !utils.IsSortedAndUnique(i.Outs) {
		return errOutputsNotSortedUnique
	}

	return nil
}

var (
	errFetchingUTXOs      = errors.New("fetching UTXOs")
	errUnmarshallingUTXO  = errors.New("unmarshalling UTXO")
	errMismatchedAssetIDs = errors.New("mismatched asset IDs")
	errVerifyingTransfer  = errors.New("verifying transfer")
)

func (i *Import) verifyCredentials(sm chainsatomic.SharedMemory, creds []Credential) error {
	if len(creds) == 0 {
		return i.verifyUnsigned(sm)
	}
	if len(i.ImportedInputs) != len(creds) {
		return fmt.Errorf("%w: want %d, got %d", errIncorrectNumCredentials, len(i.ImportedInputs), len(creds))
	}

	fxTx, err := toFxTx(i)
	if err != nil {
		return fmt.Errorf("%w: %w", errConvertingToFxTx, err)
	}

	utxoIDs := make([][]byte, len(i.ImportedInputs))
	for j, in := range i.ImportedInputs {
		inputID := in.InputID()
		utxoIDs[j] = inputID[:]
	}

	utxoBytes, err := sm.Get(i.SourceChain, utxoIDs)
	if err != nil {
		return fmt.Errorf("%w from %s: %w", errFetchingUTXOs, i.SourceChain, err)
	}

	for j, in := range i.ImportedInputs {
		// TODO(StephenButtolph): Parallelize transfer verification, which
		// includes signature verification. This is non-trivial, because
		// transactions frequently contain duplicate signatures, which are
		// currently being cached.
		utxo, err := ParseUTXO(utxoBytes[j])
		if err != nil {
			return fmt.Errorf("%w (%d): %w", errUnmarshallingUTXO, j, err)
		}
		if utxo.Asset.ID != in.Asset.ID {
			return fmt.Errorf("%w (%d): input asset %s does not match UTXO asset %s", errMismatchedAssetIDs, j, in.Asset.ID, utxo.Asset.ID)
		}
		if err := fx.VerifyTransfer(fxTx, in.In, creds[j], utxo.Out); err != nil {
			return fmt.Errorf("%w (%d): %w", errVerifyingTransfer, j, err)
		}
	}
	return nil
}

var (
	errUnsignedNotOneOutput = errors.New("unsigned import must have exactly one output")
	errUnsignedBurnTooHigh  = errors.New("unsigned import burns too much")
	errNotEVMOwned          = errors.New("UTXO is not marked as EVM-owned")
	errUnsignedOutputOwner  = errors.New("unsigned import output is not the UTXO owner")
	errUnsignedAmount       = errors.New("unsigned import input amount does not match UTXO")
)

// verifyUnsigned authorizes an [Import] with no credentials: every consumed
// UTXO must be marked with [EVMOwnerLocktime] and owned by exactly the one
// output address, so whoever issues the tx can only credit the owner.
func (i *Import) verifyUnsigned(sm chainsatomic.SharedMemory) error {
	if len(i.Outs) != 1 {
		return errUnsignedNotOneOutput
	}
	out := i.Outs[0]
	burned, err := i.burned(out.AssetID)
	if err != nil {
		return err
	}
	if burned > MaxUnsignedImportBurn {
		return fmt.Errorf("%w: %d > %d", errUnsignedBurnTooHigh, burned, MaxUnsignedImportBurn)
	}

	utxoIDs := make([][]byte, len(i.ImportedInputs))
	for j, in := range i.ImportedInputs {
		inputID := in.InputID()
		utxoIDs[j] = inputID[:]
	}
	utxoBytes, err := sm.Get(i.SourceChain, utxoIDs)
	if err != nil {
		return fmt.Errorf("%w from %s: %w", errFetchingUTXOs, i.SourceChain, err)
	}

	for j, in := range i.ImportedInputs {
		utxo, err := ParseUTXO(utxoBytes[j])
		if err != nil {
			return fmt.Errorf("%w (%d): %w", errUnmarshallingUTXO, j, err)
		}
		if utxo.Asset.ID != in.Asset.ID {
			return fmt.Errorf("%w (%d): input asset %s does not match UTXO asset %s", errMismatchedAssetIDs, j, in.Asset.ID, utxo.Asset.ID)
		}
		owner, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok || owner.Locktime != EVMOwnerLocktime || owner.Threshold != 1 || len(owner.Addrs) != 1 {
			return fmt.Errorf("%w (%d)", errNotEVMOwned, j)
		}
		if common.Address(owner.Addrs[0]) != out.Address {
			return fmt.Errorf("%w (%d): %s", errUnsignedOutputOwner, j, owner.Addrs[0])
		}
		if in.In.Amount() != owner.Amt {
			return fmt.Errorf("%w (%d): %d != %d", errUnsignedAmount, j, in.In.Amount(), owner.Amt)
		}
	}
	return nil
}

var errUnsignedDust = errors.New("unsigned import does not cover its fee")

// repriceUnsigned returns a copy of i whose single output leaves exactly the
// fee that gas at baseFee costs, rounded up. Because the tx bytes have fixed
// width, the gas does not depend on the amounts, so every builder and
// verifier derives the same tx from the same inputs and header. Whoever
// posted the tx cannot inflate the fee.
func (i *Import) repriceUnsigned(baseFee *uint256.Int) (*Import, error) {
	gas, err := gasUsed(i)
	if err != nil {
		return nil, err
	}
	var (
		fee   = new(uint256.Int).Mul(uint256.NewInt(uint64(gas)), baseFee)
		burn  = new(uint256.Int)
		total nAVAX
	)
	burn.Add(fee, x2cRate).SubUint64(burn, 1).Div(burn, x2cRate) // ceil(fee / x2cRate)
	for _, in := range i.ImportedInputs {
		total, err = math.Add(total, in.In.Amount())
		if err != nil {
			return nil, err
		}
	}
	if !burn.IsUint64() || burn.Uint64() >= total {
		return nil, fmt.Errorf("%w: %d nAVAX in, %s nAVAX fee", errUnsignedDust, total, burn)
	}
	repriced := *i
	repriced.Outs = []Output{{Address: i.Outs[0].Address, Amount: total - burn.Uint64(), AssetID: i.Outs[0].AssetID}}
	return &repriced, nil
}

var errUnexpectedInputType = errors.New("unexpected input type")

func (i *Import) numSigs() (uint64, error) {
	var n uint64
	for _, in := range i.ImportedInputs {
		input, ok := in.In.(*secp256k1fx.TransferInput)
		if !ok {
			return 0, fmt.Errorf("%w: got %T ; want %T", errUnexpectedInputType, in.In, input)
		}
		n += uint64(len(input.SigIndices))
	}
	return n, nil
}

func (i *Import) asOp(avaxAssetID ids.ID) (op, error) {
	mint := make(map[common.Address]uint256.Int, len(i.Outs))
	for _, out := range i.Outs {
		if out.AssetID != avaxAssetID {
			continue
		}

		var (
			total  = mint[out.Address]
			amount = ScaleAVAX(out.Amount)
		)
		if _, overflow := total.AddOverflow(&total, &amount); overflow {
			return op{}, fmt.Errorf("%w: for address %s", errOverflow, out.Address)
		}
		mint[out.Address] = total
	}
	return op{
		mint: mint,
	}, nil
}

func (i *Import) atomicRequests(ids.ID) (ids.ID, *chainsatomic.Requests, error) {
	utxoIDs := make([][]byte, len(i.ImportedInputs))
	for j, in := range i.ImportedInputs {
		inputID := in.InputID()
		utxoIDs[j] = inputID[:]
	}
	return i.SourceChain, &chainsatomic.Requests{RemoveRequests: utxoIDs}, nil
}

// transferNonAVAX adds the non-AVAX balances to the statedb.
func (i *Import) transferNonAVAX(avaxAssetID ids.ID, statedb *extstate.StateDB) error {
	for _, out := range i.Outs {
		if out.AssetID == avaxAssetID {
			continue
		}

		coinID := common.Hash(out.AssetID)
		amount := new(big.Int).SetUint64(out.Amount)
		statedb.AddBalanceMultiCoin(out.Address, coinID, amount)
	}
	return nil
}
