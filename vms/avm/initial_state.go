// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"bytes"
	"errors"
	"sort"

	"github.com/ava-labs/gecko/vms/components/codec"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errNilInitialState = errors.New("nil initial state is not valid")
	errNilFxOutput     = errors.New("nil feature extension output is not valid")
)

// InitialState ...
type InitialState struct {
	FxID uint32              `serialize:"true" json:"fxID"`
	Outs []verify.Verifiable `serialize:"true" json:"outputs"`
}

// Verify implements the verify.Verifiable interface
func (is *InitialState) Verify(c codec.Codec, numFxs int) error {
	switch {
	case is == nil:
		return errNilInitialState
	case is.FxID >= uint32(numFxs):
		return errUnknownFx
	}

	for _, out := range is.Outs {
		if out == nil {
			return errNilFxOutput
		}
		if err := out.Verify(); err != nil {
			return err
		}
	}
	if !isSortedVerifiables(is.Outs, c) {
		return errOutputsNotSorted
	}

	return nil
}

// Sort ...
func (is *InitialState) Sort(c codec.Codec) { sortVerifiables(is.Outs, c) }

type innerSortVerifiables struct {
	vers  []verify.Verifiable
	codec codec.Codec
}

func (vers *innerSortVerifiables) Less(i, j int) bool {
	iVer := vers.vers[i]
	jVer := vers.vers[j]

	iBytes, err := vers.codec.Marshal(&iVer)
	if err != nil {
		return false
	}
	jBytes, err := vers.codec.Marshal(&jVer)
	if err != nil {
		return false
	}
	return bytes.Compare(iBytes, jBytes) == -1
}
func (vers *innerSortVerifiables) Len() int      { return len(vers.vers) }
func (vers *innerSortVerifiables) Swap(i, j int) { v := vers.vers; v[j], v[i] = v[i], v[j] }

func sortVerifiables(vers []verify.Verifiable, c codec.Codec) {
	sort.Sort(&innerSortVerifiables{vers: vers, codec: c})
}
func isSortedVerifiables(vers []verify.Verifiable, c codec.Codec) bool {
	return sort.IsSorted(&innerSortVerifiables{vers: vers, codec: c})
}

type innerSortInitialState []*InitialState

func (iss innerSortInitialState) Less(i, j int) bool { return iss[i].FxID < iss[j].FxID }
func (iss innerSortInitialState) Len() int           { return len(iss) }
func (iss innerSortInitialState) Swap(i, j int)      { iss[j], iss[i] = iss[i], iss[j] }

func sortInitialStates(iss []*InitialState) { sort.Sort(innerSortInitialState(iss)) }
func isSortedAndUniqueInitialStates(iss []*InitialState) bool {
	return sort.IsSorted(innerSortInitialState(iss))
}
