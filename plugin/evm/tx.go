// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	errWrongBlockchainID = errors.New("wrong blockchain ID provided")
	errWrongNetworkID    = errors.New("tx was issued with a different network ID")
	errNilTx             = errors.New("tx is nil")
	errNoValueOutput     = errors.New("output has no value")
	errNoValueInput      = errors.New("input has no value")
	errNilOutput         = errors.New("nil output")
	errNilInput          = errors.New("nil input")
	errEmptyAssetID      = errors.New("empty asset ID is not valid")
	errNilBaseFee        = errors.New("cannot calculate dynamic fee with nil baseFee")
	errFeeOverflow       = errors.New("overflow occurred while calculating the fee")
)

// Constants for calculating the gas consumed by atomic transactions
var (
	TxBytesGas   uint64 = 1
	EVMOutputGas uint64 = (common.AddressLength + wrappers.LongLen + hashing.HashLen) * TxBytesGas
	EVMInputGas  uint64 = (common.AddressLength+wrappers.LongLen+hashing.HashLen+wrappers.LongLen)*TxBytesGas + secp256k1fx.CostPerSignature
)

// EVMOutput defines an output that is added to the EVM state created by import transactions
type EVMOutput struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
	AssetID ids.ID         `serialize:"true" json:"assetID"`
}

// EVMInput defines an input created from the EVM state to fund export transactions
type EVMInput struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
	AssetID ids.ID         `serialize:"true" json:"assetID"`
	Nonce   uint64         `serialize:"true" json:"nonce"`
}

// Verify ...
func (out *EVMOutput) Verify() error {
	switch {
	case out == nil:
		return errNilOutput
	case out.Amount == 0:
		return errNoValueOutput
	case out.AssetID == ids.Empty:
		return errEmptyAssetID
	}
	return nil
}

// Verify ...
func (in *EVMInput) Verify() error {
	switch {
	case in == nil:
		return errNilInput
	case in.Amount == 0:
		return errNoValueInput
	case in.AssetID == ids.Empty:
		return errEmptyAssetID
	}
	return nil
}

// UnsignedTx is an unsigned transaction
type UnsignedTx interface {
	Initialize(unsignedBytes, signedBytes []byte)
	ID() ids.ID
	GasUsed(fixedFee bool) (uint64, error)
	Burned(assetID ids.ID) (uint64, error)
	Bytes() []byte
	SignedBytes() []byte
}

// UnsignedAtomicTx is an unsigned operation that can be atomically accepted
type UnsignedAtomicTx interface {
	UnsignedTx

	// InputUTXOs returns the UTXOs this tx consumes
	InputUTXOs() set.Set[ids.ID]
	// Verify attempts to verify that the transaction is well formed
	Verify(ctx *snow.Context, rules params.Rules) error
	// Attempts to verify this transaction with the provided state.
	SemanticVerify(vm *VM, stx *Tx, parent *Block, baseFee *big.Int, rules params.Rules) error
	// AtomicOps returns the blockchainID and set of atomic requests that
	// must be applied to shared memory for this transaction to be accepted.
	// The set of atomic requests must be returned in a consistent order.
	AtomicOps() (ids.ID, *atomic.Requests, error)

	EVMStateTransfer(ctx *snow.Context, state *state.StateDB) error
}

// Tx is a signed transaction
type Tx struct {
	// The body of this transaction
	UnsignedAtomicTx `serialize:"true" json:"unsignedTx"`

	// The credentials of this transaction
	Creds []verify.Verifiable `serialize:"true" json:"credentials"`
}

// Sign this transaction with the provided signers
func (tx *Tx) Sign(c codec.Manager, signers [][]*secp256k1.PrivateKey) error {
	unsignedBytes, err := c.Marshal(codecVersion, &tx.UnsignedAtomicTx)
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedAtomicTx: %w", err)
	}

	// Attach credentials
	hash := hashing.ComputeHash256(unsignedBytes)
	for _, keys := range signers {
		cred := &secp256k1fx.Credential{
			Sigs: make([][secp256k1.SignatureLen]byte, len(keys)),
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

	signedBytes, err := c.Marshal(codecVersion, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal Tx: %w", err)
	}
	tx.Initialize(unsignedBytes, signedBytes)
	return nil
}

// BlockFeeContribution calculates how much AVAX towards the block fee contribution was paid
// for via this transaction denominated in [avaxAssetID] with [baseFee] used to calculate the
// cost of this transaction. This function also returns the [gasUsed] by the
// transaction for inclusion in the [baseFee] algorithm.
func (tx *Tx) BlockFeeContribution(fixedFee bool, avaxAssetID ids.ID, baseFee *big.Int) (*big.Int, *big.Int, error) {
	if baseFee == nil {
		return nil, nil, errNilBaseFee
	}
	if baseFee.Cmp(common.Big0) <= 0 {
		return nil, nil, fmt.Errorf("cannot calculate tip with base fee %d <= 0", baseFee)
	}
	gasUsed, err := tx.GasUsed(fixedFee)
	if err != nil {
		return nil, nil, err
	}
	txFee, err := calculateDynamicFee(gasUsed, baseFee)
	if err != nil {
		return nil, nil, err
	}
	burned, err := tx.Burned(avaxAssetID)
	if err != nil {
		return nil, nil, err
	}
	if txFee > burned {
		return nil, nil, fmt.Errorf("insufficient AVAX burned (%d) to cover import tx fee (%d)", burned, txFee)
	}
	excessBurned := burned - txFee

	// Calculate the amount of AVAX that has been burned above the required fee denominated
	// in C-Chain native 18 decimal places
	blockFeeContribution := new(big.Int).Mul(new(big.Int).SetUint64(excessBurned), x2cRate)
	return blockFeeContribution, new(big.Int).SetUint64(gasUsed), nil
}

// innerSortInputsAndSigners implements sort.Interface for EVMInput
type innerSortInputsAndSigners struct {
	inputs  []EVMInput
	signers [][]*secp256k1.PrivateKey
}

func (ins *innerSortInputsAndSigners) Less(i, j int) bool {
	addrComp := bytes.Compare(ins.inputs[i].Address.Bytes(), ins.inputs[j].Address.Bytes())
	if addrComp != 0 {
		return addrComp < 0
	}
	return bytes.Compare(ins.inputs[i].AssetID[:], ins.inputs[j].AssetID[:]) < 0
}

func (ins *innerSortInputsAndSigners) Len() int { return len(ins.inputs) }

func (ins *innerSortInputsAndSigners) Swap(i, j int) {
	ins.inputs[j], ins.inputs[i] = ins.inputs[i], ins.inputs[j]
	ins.signers[j], ins.signers[i] = ins.signers[i], ins.signers[j]
}

// SortEVMInputsAndSigners sorts the list of EVMInputs based on the addresses and assetIDs
func SortEVMInputsAndSigners(inputs []EVMInput, signers [][]*secp256k1.PrivateKey) {
	sort.Sort(&innerSortInputsAndSigners{inputs: inputs, signers: signers})
}

// IsSortedAndUniqueEVMInputs returns true if the EVM Inputs are sorted and unique
// based on the account addresses
func IsSortedAndUniqueEVMInputs(inputs []EVMInput) bool {
	return utils.IsSortedAndUnique(&innerSortInputsAndSigners{inputs: inputs})
}

// innerSortEVMOutputs implements sort.Interface for EVMOutput
type innerSortEVMOutputs struct {
	outputs []EVMOutput
}

func (outs *innerSortEVMOutputs) Less(i, j int) bool {
	addrComp := bytes.Compare(outs.outputs[i].Address.Bytes(), outs.outputs[j].Address.Bytes())
	if addrComp != 0 {
		return addrComp < 0
	}
	return bytes.Compare(outs.outputs[i].AssetID[:], outs.outputs[j].AssetID[:]) < 0
}

func (outs *innerSortEVMOutputs) Len() int { return len(outs.outputs) }

func (outs *innerSortEVMOutputs) Swap(i, j int) {
	outs.outputs[j], outs.outputs[i] = outs.outputs[i], outs.outputs[j]
}

// SortEVMOutputs sorts the list of EVMOutputs based on the addresses and assetIDs
// of the outputs
func SortEVMOutputs(outputs []EVMOutput) {
	sort.Sort(&innerSortEVMOutputs{outputs: outputs})
}

// IsSortedEVMOutputs returns true if the EVMOutputs are sorted
// based on the account addresses and assetIDs
func IsSortedEVMOutputs(outputs []EVMOutput) bool {
	return sort.IsSorted(&innerSortEVMOutputs{outputs: outputs})
}

// IsSortedAndUniqueEVMOutputs returns true if the EVMOutputs are sorted
// and unique based on the account addresses and assetIDs
func IsSortedAndUniqueEVMOutputs(outputs []EVMOutput) bool {
	return utils.IsSortedAndUnique(&innerSortEVMOutputs{outputs: outputs})
}

// calculates the amount of AVAX that must be burned by an atomic transaction
// that consumes [cost] at [baseFee].
func calculateDynamicFee(cost uint64, baseFee *big.Int) (uint64, error) {
	if baseFee == nil {
		return 0, errNilBaseFee
	}
	bigCost := new(big.Int).SetUint64(cost)
	fee := new(big.Int).Mul(bigCost, baseFee)
	feeToRoundUp := new(big.Int).Add(fee, x2cRateMinus1)
	feeInNAVAX := new(big.Int).Div(feeToRoundUp, x2cRate)
	if !feeInNAVAX.IsUint64() {
		// the fee is more than can fit in a uint64
		return 0, errFeeOverflow
	}
	return feeInNAVAX.Uint64(), nil
}

func calcBytesCost(len int) uint64 {
	return uint64(len) * TxBytesGas
}

// mergeAtomicOps merges atomic requests represented by [txs]
// to the [output] map, depending on whether [chainID] is present in the map.
func mergeAtomicOps(txs []*Tx) (map[ids.ID]*atomic.Requests, error) {
	if len(txs) > 1 {
		// txs should be stored in order of txID to ensure consistency
		// with txs initialized from the txID index.
		copyTxs := make([]*Tx, len(txs))
		copy(copyTxs, txs)
		sort.Slice(copyTxs, func(i, j int) bool { return copyTxs[i].ID().Hex() < copyTxs[j].ID().Hex() })
		txs = copyTxs
	}
	output := make(map[ids.ID]*atomic.Requests)
	for _, tx := range txs {
		chainID, txRequests, err := tx.UnsignedAtomicTx.AtomicOps()
		if err != nil {
			return nil, err
		}
		mergeAtomicOpsToMap(output, chainID, txRequests)
	}
	return output, nil
}

// mergeAtomicOps merges atomic ops for [chainID] represented by [requests]
// to the [output] map provided.
func mergeAtomicOpsToMap(output map[ids.ID]*atomic.Requests, chainID ids.ID, requests *atomic.Requests) {
	if request, exists := output[chainID]; exists {
		request.PutRequests = append(request.PutRequests, requests.PutRequests...)
		request.RemoveRequests = append(request.RemoveRequests, requests.RemoveRequests...)
	} else {
		output[chainID] = requests
	}
}
