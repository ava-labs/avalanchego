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

	height int
	status choices.Status

	bytes []byte
}

func (v *Vtx) ID() ids.ID             { return v.id }
func (v *Vtx) ParentIDs() []ids.ID    { return nil }
func (v *Vtx) Parents() []Vertex      { return v.dependencies }
func (v *Vtx) Txs() []snowstorm.Tx    { return v.txs }
func (v *Vtx) Status() choices.Status { return v.status }
func (v *Vtx) Live()                  {}
func (v *Vtx) Accept() error          { v.status = choices.Accepted; return nil }
func (v *Vtx) Reject() error          { v.status = choices.Rejected; return nil }
func (v *Vtx) Bytes() []byte          { return v.bytes }

type sortVts []*Vtx

func (sv sortVts) Less(i, j int) bool { return sv[i].height < sv[j].height }
func (sv sortVts) Len() int           { return len(sv) }
func (sv sortVts) Swap(i, j int)      { sv[j], sv[i] = sv[i], sv[j] }

func SortVts(vts []*Vtx) { sort.Sort(sortVts(vts)) }
