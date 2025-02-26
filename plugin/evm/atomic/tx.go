// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	X2CRateUint64       uint64 = 1_000_000_000
	x2cRateMinus1Uint64 uint64 = X2CRateUint64 - 1
)

var (
	ErrWrongNetworkID = errors.New("tx was issued with a different network ID")
	ErrNilTx          = errors.New("tx is nil")
	errNoValueOutput  = errors.New("output has no value")
	ErrNoValueInput   = errors.New("input has no value")
	errNilOutput      = errors.New("nil output")
	errNilInput       = errors.New("nil input")
	errEmptyAssetID   = errors.New("empty asset ID is not valid")
	errNilBaseFee     = errors.New("cannot calculate dynamic fee with nil baseFee")
	errFeeOverflow    = errors.New("overflow occurred while calculating the fee")
)

// Constants for calculating the gas consumed by atomic transactions
var (
	TxBytesGas   uint64 = 1
	EVMOutputGas uint64 = (common.AddressLength + wrappers.LongLen + hashing.HashLen) * TxBytesGas
	EVMInputGas  uint64 = (common.AddressLength+wrappers.LongLen+hashing.HashLen+wrappers.LongLen)*TxBytesGas + secp256k1fx.CostPerSignature
	// X2CRate is the conversion rate between the smallest denomination on the X-Chain
	// 1 nAVAX and the smallest denomination on the C-Chain 1 wei. Where 1 nAVAX = 1 gWei.
	// This is only required for AVAX because the denomination of 1 AVAX is 9 decimal
	// places on the X and P chains, but is 18 decimal places within the EVM.
	X2CRate       = uint256.NewInt(X2CRateUint64)
	x2cRateMinus1 = uint256.NewInt(x2cRateMinus1Uint64)
)

// EVMOutput defines an output that is added to the EVM state created by import transactions
type EVMOutput struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
	AssetID ids.ID         `serialize:"true" json:"assetID"`
}

func (o EVMOutput) Compare(other EVMOutput) int {
	addrComp := bytes.Compare(o.Address.Bytes(), other.Address.Bytes())
	if addrComp != 0 {
		return addrComp
	}
	return bytes.Compare(o.AssetID[:], other.AssetID[:])
}

// EVMInput defines an input created from the EVM state to fund export transactions
type EVMInput struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
	AssetID ids.ID         `serialize:"true" json:"assetID"`
	Nonce   uint64         `serialize:"true" json:"nonce"`
}

func (i EVMInput) Compare(other EVMInput) int {
	addrComp := bytes.Compare(i.Address.Bytes(), other.Address.Bytes())
	if addrComp != 0 {
		return addrComp
	}
	return bytes.Compare(i.AssetID[:], other.AssetID[:])
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
		return ErrNoValueInput
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

type Backend struct {
	Ctx          *snow.Context
	Fx           fx.Fx
	Rules        params.Rules
	Bootstrapped bool
	BlockFetcher BlockFetcher
	SecpCache    *secp256k1.RecoverCache
}

type BlockFetcher interface {
	LastAcceptedBlockInternal() snowman.Block
	GetBlockInternal(context.Context, ids.ID) (snowman.Block, error)
}

type AtomicBlockContext interface {
	AtomicTxs() []*Tx
	snowman.Block
}

type StateDB interface {
	AddBalance(common.Address, *uint256.Int)
	AddBalanceMultiCoin(common.Address, common.Hash, *big.Int)

	SubBalance(common.Address, *uint256.Int)
	SubBalanceMultiCoin(common.Address, common.Hash, *big.Int)

	GetBalance(common.Address) *uint256.Int
	GetBalanceMultiCoin(common.Address, common.Hash) *big.Int

	GetNonce(common.Address) uint64
	SetNonce(common.Address, uint64)
}

// UnsignedAtomicTx is an unsigned operation that can be atomically accepted
type UnsignedAtomicTx interface {
	UnsignedTx

	// InputUTXOs returns the UTXOs this tx consumes
	InputUTXOs() set.Set[ids.ID]
	// Verify attempts to verify that the transaction is well formed
	Verify(ctx *snow.Context, rules params.Rules) error
	// Attempts to verify this transaction with the provided state.
	// SemanticVerify this transaction is valid.
	SemanticVerify(backend *Backend, stx *Tx, parent AtomicBlockContext, baseFee *big.Int) error
	// AtomicOps returns the blockchainID and set of atomic requests that
	// must be applied to shared memory for this transaction to be accepted.
	// The set of atomic requests must be returned in a consistent order.
	AtomicOps() (ids.ID, *atomic.Requests, error)

	EVMStateTransfer(ctx *snow.Context, state StateDB) error
}

// Tx is a signed transaction
type Tx struct {
	// The body of this transaction
	UnsignedAtomicTx `serialize:"true" json:"unsignedTx"`

	// The credentials of this transaction
	Creds []verify.Verifiable `serialize:"true" json:"credentials"`
}

func (tx *Tx) Compare(other *Tx) int {
	txHex := tx.ID().Hex()
	otherHex := other.ID().Hex()
	switch {
	case txHex < otherHex:
		return -1
	case txHex > otherHex:
		return 1
	default:
		return 0
	}
}

// Sign this transaction with the provided signers
func (tx *Tx) Sign(c codec.Manager, signers [][]*secp256k1.PrivateKey) error {
	unsignedBytes, err := c.Marshal(CodecVersion, &tx.UnsignedAtomicTx)
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

	signedBytes, err := c.Marshal(CodecVersion, tx)
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
	txFee, err := CalculateDynamicFee(gasUsed, baseFee)
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
	blockFeeContribution := new(big.Int).Mul(new(big.Int).SetUint64(excessBurned), X2CRate.ToBig())
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

// calculates the amount of AVAX that must be burned by an atomic transaction
// that consumes [cost] at [baseFee].
func CalculateDynamicFee(cost uint64, baseFee *big.Int) (uint64, error) {
	if baseFee == nil {
		return 0, errNilBaseFee
	}
	// fee = (cost * baseFee + [X2CRate] - 1) / [X2CRate]
	fee := new(big.Int).SetUint64(cost)
	fee.Mul(fee, baseFee)
	fee.Add(fee, x2cRateMinus1.ToBig())
	fee.Div(fee, X2CRate.ToBig())
	if !fee.IsUint64() {
		// the fee is more than can fit in a uint64
		return 0, errFeeOverflow
	}
	return fee.Uint64(), nil
}

func calcBytesCost(len int) uint64 {
	return uint64(len) * TxBytesGas
}
