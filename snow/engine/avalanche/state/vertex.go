// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"errors"
	"sort"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
	"github.com/ava-labs/gecko/snow/engine/avalanche"
	"github.com/ava-labs/gecko/utils"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/wrappers"
)

// maxSize is the maximum allowed vertex size. It is necessary to deter DoS.
const maxSize = 1 << 20

var (
	errBadCodec       = errors.New("invalid codec")
	errExtraSpace     = errors.New("trailing buffer space")
	errInvalidParents = errors.New("vertex contains non-sorted or duplicated parentIDs")
	errInvalidTxs     = errors.New("vertex contains non-sorted or duplicated transactions")
	errNoTxs          = errors.New("vertex contains no transactions")
)

type vertex struct {
	id ids.ID

	chainID ids.ID
	height  uint64

	parentIDs []ids.ID
	txs       []snowstorm.Tx

	bytes []byte
}

func (vtx *vertex) ID() ids.ID    { return vtx.id }
func (vtx *vertex) Bytes() []byte { return vtx.bytes }

func (vtx *vertex) Verify() error {
	switch {
	case !ids.IsSortedAndUniqueIDs(vtx.parentIDs):
		return errInvalidParents
	case len(vtx.txs) == 0:
		return errNoTxs
	case !isSortedAndUniqueTxs(vtx.txs):
		return errInvalidTxs
	default:
		return nil
	}
}

/*
 * Vertex:
 * Codec        | 04 Bytes
 * Chain        | 32 Bytes
 * Height       | 08 Bytes
 * NumParents   | 04 Bytes
 * Repeated (NumParents):
 *     ParentID | 32 bytes
 * NumTxs       | 04 Bytes
 * Repeated (NumTxs):
 *     TxSize   | 04 bytes
 *     Tx       | ?? bytes
 */

// Marshal creates the byte representation of the vertex
func (vtx *vertex) Marshal() ([]byte, error) {
	p := wrappers.Packer{MaxSize: maxSize}

	p.PackInt(uint32(CustomID))
	p.PackFixedBytes(vtx.chainID.Bytes())
	p.PackLong(vtx.height)

	p.PackInt(uint32(len(vtx.parentIDs)))
	for _, parentID := range vtx.parentIDs {
		p.PackFixedBytes(parentID.Bytes())
	}

	p.PackInt(uint32(len(vtx.txs)))
	for _, tx := range vtx.txs {
		p.PackBytes(tx.Bytes())
	}
	return p.Bytes, p.Err
}

// Unmarshal attempts to set the contents of this vertex to the value encoded in
// the stream of bytes.
func (vtx *vertex) Unmarshal(b []byte, vm avalanche.DAGVM) error {
	p := wrappers.Packer{Bytes: b}

	if codecID := ID(p.UnpackInt()); codecID != CustomID {
		p.Add(errBadCodec)
	}

	chainID, _ := ids.ToID(p.UnpackFixedBytes(hashing.HashLen))
	height := p.UnpackLong()

	parentIDs := []ids.ID(nil)
	for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
		parentID, _ := ids.ToID(p.UnpackFixedBytes(hashing.HashLen))
		parentIDs = append(parentIDs, parentID)
	}

	txs := []snowstorm.Tx(nil)
	for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
		tx, err := vm.ParseTx(p.UnpackBytes())
		p.Add(err)
		txs = append(txs, tx)
	}

	if p.Offset != len(b) {
		p.Add(errExtraSpace)
	}

	if p.Errored() {
		return p.Err
	}

	*vtx = vertex{
		id:        ids.NewID(hashing.ComputeHash256Array(b)),
		parentIDs: parentIDs,
		chainID:   chainID,
		height:    height,
		txs:       txs,
		bytes:     b,
	}
	return nil
}

type sortTxsData []snowstorm.Tx

func (txs sortTxsData) Less(i, j int) bool {
	return bytes.Compare(txs[i].ID().Bytes(), txs[j].ID().Bytes()) == -1
}
func (txs sortTxsData) Len() int      { return len(txs) }
func (txs sortTxsData) Swap(i, j int) { txs[j], txs[i] = txs[i], txs[j] }

func sortTxs(txs []snowstorm.Tx) { sort.Sort(sortTxsData(txs)) }
func isSortedAndUniqueTxs(txs []snowstorm.Tx) bool {
	return utils.IsSortedAndUnique(sortTxsData(txs))
}
