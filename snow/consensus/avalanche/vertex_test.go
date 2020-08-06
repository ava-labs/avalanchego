// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"sort"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
)

type Vtx struct {
	dependencies []Vertex
	id           ids.ID
	txs          []snowstorm.Tx

	height uint64
	status choices.Status

	Validity error
	bytes    []byte

	Err error // Error for testing
}

func (v *Vtx) ID() ids.ID                   { return v.id }
func (v *Vtx) ParentIDs() []ids.ID          { return nil }
func (v *Vtx) Parents() ([]Vertex, error)   { return v.dependencies, v.Err }
func (v *Vtx) Height() (uint64, error)      { return v.height, v.Err }
func (v *Vtx) Txs() ([]snowstorm.Tx, error) { return v.txs, v.Err }
func (v *Vtx) Status() choices.Status       { return v.status }
func (v *Vtx) Accept() error                { v.status = choices.Accepted; return v.Validity }
func (v *Vtx) Reject() error                { v.status = choices.Rejected; return v.Validity }
func (v *Vtx) Bytes() []byte                { return v.bytes }

type sortVts []*Vtx

func (sv sortVts) Less(i, j int) bool { return sv[i].height < sv[j].height }
func (sv sortVts) Len() int           { return len(sv) }
func (sv sortVts) Swap(i, j int)      { sv[j], sv[i] = sv[i], sv[j] }

func SortVts(vts []*Vtx) { sort.Sort(sortVts(vts)) }
