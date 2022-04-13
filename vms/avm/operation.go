// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"errors"
	"sort"

	"github.com/chain4travel/caminogo/codec"
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/utils"
	"github.com/chain4travel/caminogo/utils/crypto"
	"github.com/chain4travel/caminogo/vms/components/avax"
	"github.com/chain4travel/caminogo/vms/components/verify"
)

var (
	errNilOperation              = errors.New("nil operation is not valid")
	errNilFxOperation            = errors.New("nil fx operation is not valid")
	errNotSortedAndUniqueUTXOIDs = errors.New("utxo IDs not sorted and unique")
)

type Operation struct {
	avax.Asset `serialize:"true"`
	UTXOIDs    []*avax.UTXOID `serialize:"true" json:"inputIDs"`
	FxID       ids.ID         `serialize:"false" json:"fxID"`
	Op         FxOperation    `serialize:"true" json:"operation"`
}

func (op *Operation) Verify(c codec.Manager) error {
	switch {
	case op == nil:
		return errNilOperation
	case op.Op == nil:
		return errNilFxOperation
	case !avax.IsSortedAndUniqueUTXOIDs(op.UTXOIDs):
		return errNotSortedAndUniqueUTXOIDs
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

	iBytes, err := ops.codec.Marshal(codecVersion, iOp)
	if err != nil {
		return false
	}
	jBytes, err := ops.codec.Marshal(codecVersion, jOp)
	if err != nil {
		return false
	}
	return bytes.Compare(iBytes, jBytes) == -1
}
func (ops *innerSortOperation) Len() int      { return len(ops.ops) }
func (ops *innerSortOperation) Swap(i, j int) { o := ops.ops; o[j], o[i] = o[i], o[j] }

func SortOperations(ops []*Operation, c codec.Manager) {
	sort.Sort(&innerSortOperation{ops: ops, codec: c})
}

func isSortedAndUniqueOperations(ops []*Operation, c codec.Manager) bool {
	return utils.IsSortedAndUnique(&innerSortOperation{ops: ops, codec: c})
}

type innerSortOperationsWithSigners struct {
	ops     []*Operation
	signers [][]*crypto.PrivateKeySECP256K1R
	codec   codec.Manager
}

func (ops *innerSortOperationsWithSigners) Less(i, j int) bool {
	iOp := ops.ops[i]
	jOp := ops.ops[j]

	iBytes, err := ops.codec.Marshal(codecVersion, iOp)
	if err != nil {
		return false
	}
	jBytes, err := ops.codec.Marshal(codecVersion, jOp)
	if err != nil {
		return false
	}
	return bytes.Compare(iBytes, jBytes) == -1
}
func (ops *innerSortOperationsWithSigners) Len() int { return len(ops.ops) }
func (ops *innerSortOperationsWithSigners) Swap(i, j int) {
	ops.ops[j], ops.ops[i] = ops.ops[i], ops.ops[j]
	ops.signers[j], ops.signers[i] = ops.signers[i], ops.signers[j]
}

func sortOperationsWithSigners(ops []*Operation, signers [][]*crypto.PrivateKeySECP256K1R, codec codec.Manager) {
	sort.Sort(&innerSortOperationsWithSigners{ops: ops, signers: signers, codec: codec})
}
