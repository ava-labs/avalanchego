// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"errors"
	"sort"

	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/vms/components/codec"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errNilOperation   = errors.New("nil operation is not valid")
	errEmptyOperation = errors.New("empty operation is not valid")
)

// Operation ...
type Operation struct {
	Asset `serialize:"true"`

	Ins  []*OperableInput    `serialize:"true"`
	Outs []verify.Verifiable `serialize:"true"`
}

// Verify implements the verify.Verifiable interface
func (op *Operation) Verify(c codec.Codec) error {
	switch {
	case op == nil:
		return errNilOperation
	case len(op.Ins) == 0 && len(op.Outs) == 0:
		return errEmptyOperation
	}

	for _, in := range op.Ins {
		if err := in.Verify(); err != nil {
			return err
		}
	}
	if !isSortedAndUniqueOperableInputs(op.Ins) {
		return errInputsNotSortedUnique
	}

	for _, out := range op.Outs {
		if err := out.Verify(); err != nil {
			return err
		}
	}
	if !isSortedVerifiables(op.Outs, c) {
		return errOutputsNotSorted
	}

	return op.Asset.Verify()
}

type innerSortOperation struct {
	ops   []*Operation
	codec codec.Codec
}

func (ops *innerSortOperation) Less(i, j int) bool {
	iOp := ops.ops[i]
	jOp := ops.ops[j]

	iBytes, err := ops.codec.Marshal(iOp)
	if err != nil {
		return false
	}
	jBytes, err := ops.codec.Marshal(jOp)
	if err != nil {
		return false
	}
	return bytes.Compare(iBytes, jBytes) == -1
}
func (ops *innerSortOperation) Len() int      { return len(ops.ops) }
func (ops *innerSortOperation) Swap(i, j int) { o := ops.ops; o[j], o[i] = o[i], o[j] }

func sortOperations(ops []*Operation, c codec.Codec) {
	sort.Sort(&innerSortOperation{ops: ops, codec: c})
}
func isSortedAndUniqueOperations(ops []*Operation, c codec.Codec) bool {
	return utils.IsSortedAndUnique(&innerSortOperation{ops: ops, codec: c})
}
