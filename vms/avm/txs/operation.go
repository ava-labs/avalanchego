// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"errors"
	"sort"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	ErrNilOperation              = errors.New("nil operation is not valid")
	ErrNilFxOperation            = errors.New("nil fx operation is not valid")
	ErrNotSortedAndUniqueUTXOIDs = errors.New("utxo IDs not sorted and unique")
)

type Operation struct {
	avax.Asset `serialize:"true"`
	UTXOIDs    []*avax.UTXOID  `serialize:"true" json:"inputIDs"`
	FxID       ids.ID          `serialize:"false" json:"fxID"`
	Op         fxs.FxOperation `serialize:"true" json:"operation"`
}

func (op *Operation) Verify() error {
	switch {
	case op == nil:
		return ErrNilOperation
	case op.Op == nil:
		return ErrNilFxOperation
	case !utils.IsSortedAndUniqueSortable(op.UTXOIDs):
		return ErrNotSortedAndUniqueUTXOIDs
	default:
		return verify.All(&op.Asset, op.Op)
	}
}

type innerSortOperation struct {
	ops   []*Operation
	codec codec.Manager
}

func (ops *innerSortOperation) Less(i, j int) bool {
	iOp := ops.ops[i]
	jOp := ops.ops[j]

	iBytes, err := ops.codec.Marshal(CodecVersion, iOp)
	if err != nil {
		return false
	}
	jBytes, err := ops.codec.Marshal(CodecVersion, jOp)
	if err != nil {
		return false
	}
	return bytes.Compare(iBytes, jBytes) == -1
}

func (ops *innerSortOperation) Len() int {
	return len(ops.ops)
}

func (ops *innerSortOperation) Swap(i, j int) {
	o := ops.ops
	o[j], o[i] = o[i], o[j]
}

func SortOperations(ops []*Operation, c codec.Manager) {
	sort.Sort(&innerSortOperation{ops: ops, codec: c})
}

func IsSortedAndUniqueOperations(ops []*Operation, c codec.Manager) bool {
	return utils.IsSortedAndUnique(&innerSortOperation{ops: ops, codec: c})
}

type innerSortOperationsWithSigners struct {
	ops     []*Operation
	signers [][]*secp256k1.PrivateKey
	codec   codec.Manager
}

func (ops *innerSortOperationsWithSigners) Less(i, j int) bool {
	iOp := ops.ops[i]
	jOp := ops.ops[j]

	iBytes, err := ops.codec.Marshal(CodecVersion, iOp)
	if err != nil {
		return false
	}
	jBytes, err := ops.codec.Marshal(CodecVersion, jOp)
	if err != nil {
		return false
	}
	return bytes.Compare(iBytes, jBytes) == -1
}

func (ops *innerSortOperationsWithSigners) Len() int {
	return len(ops.ops)
}

func (ops *innerSortOperationsWithSigners) Swap(i, j int) {
	ops.ops[j], ops.ops[i] = ops.ops[i], ops.ops[j]
	ops.signers[j], ops.signers[i] = ops.signers[i], ops.signers[j]
}

func SortOperationsWithSigners(ops []*Operation, signers [][]*secp256k1.PrivateKey, codec codec.Manager) {
	sort.Sort(&innerSortOperationsWithSigners{ops: ops, signers: signers, codec: codec})
}
