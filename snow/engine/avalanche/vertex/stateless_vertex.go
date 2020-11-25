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
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const (
	// maxSize is the maximum allowed vertex size. It is necessary to deter DoS
	maxSize = 1 << 20

	// noEpochTransitionsCodecVersion is the codec version that was used when
	// there were no epoch transitions
	noEpochTransitionsCodecVersion = uint16(0)

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
)

type StatelessVertex struct {
	ChainID   ids.ID   `serialize:"true" json:"chainID"`
	Height    uint64   `serialize:"true" json:"height"`
	Epoch     uint32   `serialize:"true" json:"epoch"`
	ParentIDs []ids.ID `serialize:"true" json:"parentIDs"`
	Txs       [][]byte `serialize:"true" json:"txs"`
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
	case !isSortedAndUniqueHashOf(v.Txs):
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

func sortHashOf(bytesSlice [][]byte) { sort.Sort(sortHashOfData(bytesSlice)) }
func isSortedAndUniqueHashOf(bytesSlice [][]byte) bool {
	return utils.IsSortedAndUnique(sortHashOfData(bytesSlice))
}
