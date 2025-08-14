// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"cmp"
	"errors"
	"sort"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	ErrNilInitialState  = errors.New("nil initial state is not valid")
	ErrNilFxOutput      = errors.New("nil feature extension output is not valid")
	ErrOutputsNotSorted = errors.New("outputs not sorted")
	ErrUnknownFx        = errors.New("unknown feature extension")

	_ utils.Sortable[*InitialState] = (*InitialState)(nil)
)

type InitialState struct {
	FxIndex uint32         `serialize:"true"  json:"fxIndex"`
	FxID    ids.ID         `serialize:"false" json:"fxID"`
	Outs    []verify.State `serialize:"true"  json:"outputs"`
}

func (is *InitialState) InitCtx(ctx *snow.Context) {
	for _, out := range is.Outs {
		out.InitCtx(ctx)
	}
}

func (is *InitialState) Verify(c codec.Manager, numFxs int) error {
	switch {
	case is == nil:
		return ErrNilInitialState
	case is.FxIndex >= uint32(numFxs):
		return ErrUnknownFx
	}

	for _, out := range is.Outs {
		if out == nil {
			return ErrNilFxOutput
		}
		if err := out.Verify(); err != nil {
			return err
		}
	}
	if !isSortedState(is.Outs, c) {
		return ErrOutputsNotSorted
	}

	return nil
}

func (is *InitialState) Compare(other *InitialState) int {
	return cmp.Compare(is.FxIndex, other.FxIndex)
}

func (is *InitialState) Sort(c codec.Manager) {
	sortState(is.Outs, c)
}

type innerSortState struct {
	vers  []verify.State
	codec codec.Manager
}

func (vers *innerSortState) Less(i, j int) bool {
	iVer := vers.vers[i]
	jVer := vers.vers[j]

	iBytes, err := vers.codec.Marshal(CodecVersion, &iVer)
	if err != nil {
		return false
	}
	jBytes, err := vers.codec.Marshal(CodecVersion, &jVer)
	if err != nil {
		return false
	}
	return bytes.Compare(iBytes, jBytes) == -1
}

func (vers *innerSortState) Len() int {
	return len(vers.vers)
}

func (vers *innerSortState) Swap(i, j int) {
	v := vers.vers
	v[j], v[i] = v[i], v[j]
}

func sortState(vers []verify.State, c codec.Manager) {
	sort.Sort(&innerSortState{vers: vers, codec: c})
}

func isSortedState(vers []verify.State, c codec.Manager) bool {
	return sort.IsSorted(&innerSortState{vers: vers, codec: c})
}
