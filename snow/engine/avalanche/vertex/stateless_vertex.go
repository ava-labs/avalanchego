// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vertex

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/codec"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	// maxSize is the maximum allowed vertex size. It is necessary to deter DoS
	maxSize = 1 << 20

	// noEpochTransitionsCodecVersion is the codec version that was used when
	// there were no epoch transitions
	noEpochTransitionsCodecVersion = uint16(0)

	// apricotCodecVersion is the codec version that was used when we added
	// epoch transitions
	apricotCodecVersion = uint16(1)

	// maxNumParents is the max number of parents a vertex may have
	maxNumParents = 128

	// maxTxsPerVtx is the max number of transactions a vertex may have
	maxTxsPerVtx = 128
)

var (
	errBadEpoch         = errors.New("invalid epoch")
	errTooManyparentIDs = fmt.Errorf("vertex contains more than %d parentIDs", maxNumParents)
	errNoTxs            = errors.New("vertex contains no transactions")
	errTooManyTxs       = fmt.Errorf("vertex contains more than %d transactions", maxTxsPerVtx)
	errInvalidParents   = errors.New("vertex contains non-sorted or duplicated parentIDs")
	errInvalidTxs       = errors.New("vertex contains non-sorted or duplicated transactions")

	Codec codec.Manager
)

func init() {
	codecV0 := codec.New("serializeV0", maxSize)
	codecV1 := codec.New("serializeV1", maxSize)
	Codec = codec.NewManager(maxSize)

	errs := wrappers.Errs{}
	errs.Add(
		Codec.RegisterCodec(noEpochTransitionsCodecVersion, codecV0),
		Codec.RegisterCodec(apricotCodecVersion, codecV1),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

type StatelessVertex struct {
	ChainID      ids.ID   `serializeV0:"true" serializeV1:"true" json:"chainID"`
	Height       uint64   `serializeV0:"true" serializeV1:"true" json:"height"`
	Epoch        uint32   `serializeV0:"true" serializeV1:"true" json:"epoch"`
	ParentIDs    []ids.ID `serializeV0:"true" serializeV1:"true" json:"parentIDs"`
	Txs          [][]byte `serializeV0:"true" serializeV1:"true" json:"txs"`
	Restrictions []ids.ID `serializeV1:"true" json:"restrictions"`
}

func (v *StatelessVertex) Verify() error {
	switch {
	case v.Epoch != 0:
		return errBadEpoch
	case len(v.ParentIDs) > maxNumParents:
		return errTooManyparentIDs
	case len(v.Txs) == 0:
		return errNoTxs
	case len(v.Txs) > maxTxsPerVtx:
		return errTooManyTxs
	case !ids.IsSortedAndUniqueIDs(v.ParentIDs):
		return errInvalidParents
	case !IsSortedAndUniqueHashOf(v.Txs):
		return errInvalidTxs
	default:
		return nil
	}
}

type sortHashOfData [][]byte

func (d sortHashOfData) Less(i, j int) bool {
	return bytes.Compare(
		hashing.ComputeHash256(d[i]),
		hashing.ComputeHash256(d[j]),
	) == -1
}
func (d sortHashOfData) Len() int      { return len(d) }
func (d sortHashOfData) Swap(i, j int) { d[j], d[i] = d[i], d[j] }

func SortHashOf(bytesSlice [][]byte) { sort.Sort(sortHashOfData(bytesSlice)) }
func IsSortedAndUniqueHashOf(bytesSlice [][]byte) bool {
	return utils.IsSortedAndUnique(sortHashOfData(bytesSlice))
}
