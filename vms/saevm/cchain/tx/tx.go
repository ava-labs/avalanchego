// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package tx defines the Avalanche-specific transaction types used on the
// C-Chain to interact with the shared memory between the C-Chain and other
// chains on the Primary Network.
package tx

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/holiman/uint256"

	// Imported for [atomic.TxBytesGas] comment resolution.
	_ "github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"

	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	chainsatomic "github.com/ava-labs/avalanchego/chains/atomic"
)

var (
	errWrongNetworkID          = errors.New("wrong network ID")
	errWrongChainID            = errors.New("wrong chain ID")
	errNoInputs                = errors.New("no inputs")
	errNoOutputs               = errors.New("no outputs")
	errNotSameSubnet           = errors.New("not same subnet")
	errInvalidInput            = errors.New("invalid input")
	errNonAVAXInput            = errors.New("input contains non-AVAX")
	errInvalidOutput           = errors.New("invalid output")
	errNonAVAXOutput           = errors.New("output contains non-AVAX")
	errFlowCheckFailed         = errors.New("flow check failed")
	errInputsNotSortedUnique   = errors.New("inputs not sorted and unique")
	errOverflow                = errors.New("amount overflow")
	errIncorrectNumCredentials = errors.New("incorrect number of credentials")
	errConvertingToFxTx        = errors.New("converting to fx transaction")
)

// Tx is a signed transaction that interacts with shared memory.
// The [Unsigned] body can be implemented by either [Export] or [Import].
// The [Credential] values are implemented by [secp256k1fx.Credential].
type Tx struct {
	Unsigned `serialize:"true" json:"unsignedTx"`
	Creds    []Credential `serialize:"true" json:"credentials"`
}

// Unsigned is a common interface implemented by [Import] and [Export].
type Unsigned interface {
	// InputIDs returns the one-time-use inputs consumed by this transaction.
	//
	// [Import] transactions return consumed UTXOIDs.
	// [Export] transactions return Account+Nonce pairs.
	InputIDs() set.Set[ids.ID]

	// SanityCheck verifies that the transaction's structural invariants hold
	// against the chain's context and that it does not produce more funds
	// than it consumes.
	//
	// It does not verify signatures, whether UTXOs exist, or whether the
	// transaction performs a valid EVM state transition.
	SanityCheck(ctx *snow.Context) error

	// TransferNonAVAX transfers the non-AVAX balances requested by this
	// transaction.
	//
	// Non-AVAX transfers were only allowed prior to the Banff upgrade.
	TransferNonAVAX(avaxAssetID ids.ID, statedb *extstate.StateDB) error

	// burned returns the amount of assetID that is consumed but not produced by
	// this transaction.
	burned(assetID ids.ID) (uint64, error)

	// numSigs returns the expected number of signatures required to sign this
	// transaction.
	numSigs() (uint64, error)

	// asOp returns the operation that this transaction performs on the
	// EVM-native state. Ops do not include any non-AVAX balance changes.
	asOp(avaxAssetID ids.ID) (op, error)

	// atomicRequests returns the operations that should be applied to shared
	// memory when this transaction is executed.
	atomicRequests(txID ids.ID) (chainID ids.ID, r *chainsatomic.Requests, err error)

	// verifyCredentials verifies that the transaction is authorized by the
	// provided credentials.
	verifyCredentials(sm chainsatomic.SharedMemory, creds []Credential) error
}

// op contains the state changes of [hook.Op]
type op struct {
	burn map[common.Address]hook.AccountDebit
	mint map[common.Address]uint256.Int
}

// Credential is used in [Tx] to authorize an input of a transaction.
//
// It is only implemented by [secp256k1fx.Credential]. An interface must be used
// to correctly produce the canonical binary format during serialization.
type Credential interface {
	Self() *secp256k1fx.Credential
}

// ID returns the unique hash of the transaction.
func (t *Tx) ID() ids.ID {
	// TODO(StephenButtolph): Optimize ID by caching previously calculated
	// values.
	bytes, err := t.Bytes()
	// This error can happen, but only with invalid transactions. To avoid
	// polluting the interface, we represent all invalid transactions with
	// the zero ID.
	if err != nil {
		return ids.ID{}
	}
	return hashing.ComputeHash256Array(bytes)
}

// Bytes returns the canonical binary format of the transaction.
func (t *Tx) Bytes() ([]byte, error) {
	// TODO(StephenButtolph): Optimize Bytes by caching previously calculated
	// values.
	return c.Marshal(codecVersion, t)
}

// AsOp converts the transaction into a [hook.Op] that can be processed by SAE.
//
// The operation only includes state changes that impact Ethereum-native state.
// It does not include non-AVAX balance changes or shared memory modifications.
func (t *Tx) AsOp(avaxAssetID ids.ID) (hook.Op, error) {
	gas, err := gasUsed(t.Unsigned)
	if err != nil {
		return hook.Op{}, fmt.Errorf("calculating gas used: %w", err)
	}

	burned, err := t.burned(avaxAssetID)
	if err != nil {
		return hook.Op{}, fmt.Errorf("calculating amount burned: %w", err)
	}

	op, err := t.asOp(avaxAssetID)
	if err != nil {
		return hook.Op{}, fmt.Errorf("converting to operation: %w", err)
	}

	return hook.Op{
		ID:        t.ID(),
		Gas:       gas,
		GasFeeCap: gasPrice(burned, gas),
		Burn:      op.burn,
		Mint:      op.mint,
	}, nil
}

const (
	// intrinsicGas is an initial static amount of gas that every [Tx] must pay.
	intrinsicGas = ap5.AtomicTxIntrinsicGas
	// gasPerByte is an additional amount of gas that is charged per-byte of an
	// [Unsigned] transaction.
	gasPerByte = 1 // [atomic.TxBytesGas]
	// gasPerSig is an additional amount of gas that is charged per-signature
	// included in a [Tx].
	gasPerSig = gas.Gas(secp256k1fx.CostPerSignature)
)

func gasUsed(t Unsigned) (gas.Gas, error) {
	// We MUST provide a pointer to t so that the returned size includes the
	// type ID.
	numBytes, err := c.Size(codecVersion, &t)
	if err != nil {
		return 0, err
	}
	bytesGas, err := math.Mul(gas.Gas(numBytes), gasPerByte) //#nosec G115 -- Known non-negative
	if err != nil {
		return 0, err
	}
	numSigs, err := t.numSigs()
	if err != nil {
		return 0, err
	}
	sigsGas, err := math.Mul(gas.Gas(numSigs), gasPerSig)
	if err != nil {
		return 0, err
	}
	dynamicGas, err := math.Add(bytesGas, sigsGas)
	if err != nil {
		return 0, err
	}
	return math.Add(intrinsicGas, dynamicGas)
}

const _x2cRate = 1_000_000_000

// x2cRate is the conversion rate between the smallest denomination on the
// X-Chain, 1 nAVAX, and the smallest denomination on the C-Chain 1 aAVAX.
var x2cRate = uint256.NewInt(_x2cRate)

// scaleAVAX converts an amount denominated in nAVAX into the C-Chain's aAVAX
// denomination.
func scaleAVAX(nAVAX uint64) uint256.Int {
	var aAVAX uint256.Int
	aAVAX.SetUint64(nAVAX)
	aAVAX.Mul(&aAVAX, x2cRate)
	return aAVAX
}

// gasPrice takes in the cost, in nAVAX, and the gas and returns the price per
// gas in aAVAX/gas. It assumes gas is non-zero.
//
// The result is rounded down to the nearest aAVAX/gas.
func gasPrice(cost uint64, gas gas.Gas) uint256.Int {
	var u uint256.Int
	u.SetUint64(uint64(gas))

	p := scaleAVAX(cost)
	p.Div(&p, &u)
	return p
}

// AtomicRequests returns shared-memory modifications that this transaction
// should perform on the peer chainID during execution.
func (t *Tx) AtomicRequests() (chainID ids.ID, r *chainsatomic.Requests, err error) {
	return t.atomicRequests(t.ID())
}

// VerifyCredentials verifies that the transaction is properly authorized.
func (t *Tx) VerifyCredentials(sm chainsatomic.SharedMemory) error {
	return t.Unsigned.verifyCredentials(sm, t.Creds)
}

// Parse deserializes a [Tx] from its canonical binary format.
func Parse(b []byte) (*Tx, error) {
	var tx Tx
	if _, err := c.Unmarshal(b, &tx); err != nil {
		return nil, err
	}
	return &tx, nil
}
