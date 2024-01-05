// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Codec does serialization and deserialization
var Codec codec.Manager

// TODO: Remove after v1.11.x has activated
//
// Invariant: InitCodec and Codec must not be accessed concurrently
func InitCodec(durangoTime time.Time) error {
	var (
		lc       = linearcodec.NewDefault(durangoTime)
		newCodec = codec.NewDefaultManager()
		errs     = wrappers.Errs{}
	)
	errs.Add(
		lc.RegisterType(&UnsignedImportTx{}),
		lc.RegisterType(&UnsignedExportTx{}),
	)
	lc.SkipRegistrations(3)
	errs.Add(
		lc.RegisterType(&secp256k1fx.TransferInput{}),
		lc.RegisterType(&secp256k1fx.MintOutput{}),
		lc.RegisterType(&secp256k1fx.TransferOutput{}),
		lc.RegisterType(&secp256k1fx.MintOperation{}),
		lc.RegisterType(&secp256k1fx.Credential{}),
		lc.RegisterType(&secp256k1fx.Input{}),
		lc.RegisterType(&secp256k1fx.OutputOwners{}),
		newCodec.RegisterCodec(codecVersion, lc),
	)
	if errs.Errored() {
		return errs.Err
	}

	Codec = newCodec
	return nil
}

func init() {
	if err := InitCodec(time.Time{}); err != nil {
		panic(err)
	}
}

// extractAtomicTxs returns the atomic transactions in [atomicTxBytes] if
// they exist.
// if [batch] is true, it attempts to unmarshal [atomicTxBytes] as a slice of
// transactions (post-ApricotPhase5), and if it is false, then it unmarshals
// it as a single atomic transaction.
func ExtractAtomicTxs(atomicTxBytes []byte, batch bool, codec codec.Manager) ([]*Tx, error) {
	if len(atomicTxBytes) == 0 {
		return nil, nil
	}

	if !batch {
		tx, err := ExtractAtomicTx(atomicTxBytes, codec)
		if err != nil {
			return nil, err
		}
		return []*Tx{tx}, err
	}
	return ExtractAtomicTxsBatch(atomicTxBytes, codec)
}

// [ExtractAtomicTx] extracts a singular atomic transaction from [atomicTxBytes]
// and returns a slice of atomic transactions for compatibility with the type returned post
// ApricotPhase5.
// Note: this function assumes [atomicTxBytes] is non-empty.
func ExtractAtomicTx(atomicTxBytes []byte, codec codec.Manager) (*Tx, error) {
	atomicTx := new(Tx)
	if _, err := codec.Unmarshal(atomicTxBytes, atomicTx); err != nil {
		return nil, fmt.Errorf("failed to unmarshal atomic transaction (pre-AP5): %w", err)
	}
	if err := atomicTx.Sign(codec, nil); err != nil {
		return nil, fmt.Errorf("failed to initialize singleton atomic tx due to: %w", err)
	}
	return atomicTx, nil
}

// [ExtractAtomicTxsBatch] extracts a slice of atomic transactions from [atomicTxBytes].
// Note: this function assumes [atomicTxBytes] is non-empty.
func ExtractAtomicTxsBatch(atomicTxBytes []byte, codec codec.Manager) ([]*Tx, error) {
	var atomicTxs []*Tx
	if _, err := codec.Unmarshal(atomicTxBytes, &atomicTxs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal atomic tx (AP5) due to %w", err)
	}

	// Do not allow non-empty extra data field to contain zero atomic transactions. This would allow
	// people to construct a block that contains useless data.
	if len(atomicTxs) == 0 {
		return nil, errMissingAtomicTxs
	}

	for index, atx := range atomicTxs {
		if err := atx.Sign(codec, nil); err != nil {
			return nil, fmt.Errorf("failed to initialize atomic tx at index %d: %w", index, err)
		}
	}
	return atomicTxs, nil
}
