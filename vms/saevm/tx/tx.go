// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/strevm/hook"
	"github.com/holiman/uint256"
)

type Unsigned interface {
	// InputUTXOs returns the UTXOIDs of the inputs of this transaction.
	InputUTXOs() set.Set[ids.ID]

	// Burned returns the amount of assetID that is consumed but not produced by
	// this transaction.
	Burned(assetID ids.ID) (uint64, error)

	// SanityCheck performs basic validation on the transaction.
	SanityCheck(ctx context.Context, snowCtx *snow.Context) error

	// VerifyCredentials verifies that the transaction is authorized by the
	// provided credentials.
	VerifyCredentials(snowCtx *snow.Context, creds []verify.Verifiable) error

	// AsOp returns the operation that this transaction performs on the EVM
	// state.
	AsOp(avaxAssetID ids.ID) (
		burn map[common.Address]hook.AccountDebit,
		mint map[common.Address]uint256.Int,
		err error,
	)
}

type Tx struct {
	Unsigned `serialize:"true" json:"unsignedTx"`
	Creds    []verify.Verifiable `serialize:"true" json:"credentials"`
}

func Parse(b []byte) (*Tx, error) {
	tx := &Tx{}
	if _, err := c.Unmarshal(b, &tx); err != nil {
		return nil, fmt.Errorf("%T.Unmarshal(txBytes): %w", c, err)
	}
	return tx, nil
}

func (t *Tx) Bytes() ([]byte, error) {
	return c.Marshal(codecVersion, t)
}

func (t *Tx) ID() (ids.ID, error) {
	bytes, err := t.Bytes()
	if err != nil {
		return ids.ID{}, err
	}
	return hashing.ComputeHash256Array(bytes), nil
}

const (
	IntrinsicGas = ap5.AtomicTxIntrinsicGas
	GasPerByte   = 1 // atomic.TxBytesGas
	GasPerSig    = secp256k1fx.CostPerSignature
)

var errUnknownCredentialType = errors.New("unknown credential type")

func (t *Tx) GasUsed() (uint64, error) {
	size, err := c.Size(codecVersion, &t.Unsigned)
	if err != nil {
		return 0, err
	}
	bytesGas, err := math.Mul(uint64(size), GasPerByte)
	if err != nil {
		return 0, err
	}

	var numSigs uint64
	for _, credIntf := range t.Creds {
		cred, ok := credIntf.(*secp256k1fx.Credential)
		if !ok {
			return 0, fmt.Errorf("%w: %T", errUnknownCredentialType, credIntf)
		}

		numSigs, err = math.Add(numSigs, uint64(len(cred.Sigs)))
		if err != nil {
			return 0, err
		}
	}
	sigsGas, err := math.Mul(numSigs, GasPerSig)
	if err != nil {
		return 0, err
	}

	dynamicGas, err := math.Add(bytesGas, sigsGas)
	if err != nil {
		return 0, err
	}
	return math.Add(IntrinsicGas, dynamicGas)
}

const x2cRateUint64 = 1_000_000_000

// x2cRate is the conversion rate between the smallest denomination on the
// X-Chain, 1 nAVAX, and the smallest denomination on the C-Chain 1 aAVAX.
var x2cRate = uint256.NewInt(x2cRateUint64)

// GasPrice returns the price per gas that the transaction is paying denominated
// in aAVAX/gas.
//
// The result is rounded down to the nearest aAVAX/gas.
func (t *Tx) GasPrice(avaxAssetID ids.ID) (uint256.Int, error) {
	gasUsed, err := t.GasUsed()
	if err != nil {
		return uint256.Int{}, err
	}
	burned, err := t.Burned(avaxAssetID)
	if err != nil {
		return uint256.Int{}, err
	}

	var bigGasUsed uint256.Int
	bigGasUsed.SetUint64(gasUsed)

	var gasPrice uint256.Int // gasPrice = burned * x2cRate / gasUsed
	gasPrice.SetUint64(burned)
	gasPrice.Mul(&gasPrice, x2cRate)
	gasPrice.Div(&gasPrice, &bigGasUsed)
	return gasPrice, nil
}

func (t *Tx) Verify(ctx context.Context, snowCtx *snow.Context) error {
	if err := t.SanityCheck(ctx, snowCtx); err != nil {
		return fmt.Errorf("failed sanity check: %w", err)
	}
	if err := t.VerifyCredentials(snowCtx, t.Creds); err != nil {
		return fmt.Errorf("failed to verify credentials: %w", err)
	}
	return nil
}

func (t *Tx) AsOp(avaxAssetID ids.ID) (hook.Op, error) {
	id, err := t.ID()
	if err != nil {
		return hook.Op{}, fmt.Errorf("problem getting transaction ID: %w", err)
	}

	gasUsed, err := t.GasUsed()
	if err != nil {
		return hook.Op{}, fmt.Errorf("problem calculating gas used: %w", err)
	}

	gasPrice, err := t.GasPrice(avaxAssetID)
	if err != nil {
		return hook.Op{}, fmt.Errorf("problem calculating gas price: %w", err)
	}

	burn, mint, err := t.Unsigned.AsOp(avaxAssetID)
	if err != nil {
		return hook.Op{}, fmt.Errorf("problem converting unsigned transaction to operation: %w", err)
	}

	return hook.Op{
		ID:        id,
		Gas:       gas.Gas(gasUsed),
		GasFeeCap: gasPrice,
		Burn:      burn,
		Mint:      mint,
	}, nil
}

func MarshalSlice(txs []*Tx) ([]byte, error) {
	if len(txs) == 0 {
		return nil, nil
	}
	return c.Marshal(codecVersion, &txs)
}

var errInefficientSlicePacking = errors.New("inefficient slice packing: empty slices should be packed as nil")

func ParseSlice(b []byte) ([]*Tx, error) {
	if len(b) == 0 {
		return nil, nil
	}

	var txs []*Tx
	if _, err := c.Unmarshal(b, &txs); err != nil {
		return nil, err
	}
	if len(txs) == 0 {
		return nil, errInefficientSlicePacking
	}
	return txs, nil
}
