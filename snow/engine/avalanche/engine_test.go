// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"sort"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
)

var (
	Genesis = GenerateID()
	offset  = uint64(0)
)

func GenerateID() ids.ID {
	offset++
	return ids.Empty.Prefix(offset)
}

type Vtx struct {
	parents []avalanche.Vertex
	id      ids.ID
	txs     []snowstorm.Tx

	height int
	status choices.Status

	bytes []byte
}

func (v *Vtx) ID() ids.ID                  { return v.id }
func (v *Vtx) DependencyIDs() []ids.ID     { return nil }
func (v *Vtx) Parents() []avalanche.Vertex { return v.parents }
func (v *Vtx) Txs() []snowstorm.Tx         { return v.txs }
func (v *Vtx) Status() choices.Status      { return v.status }
func (v *Vtx) Accept() error               { v.status = choices.Accepted; return nil }
func (v *Vtx) Reject() error               { v.status = choices.Rejected; return nil }
func (v *Vtx) Bytes() []byte               { return v.bytes }

type sortVts []*Vtx

func (sv sortVts) Less(i, j int) bool { return sv[i].height < sv[j].height }
func (sv sortVts) Len() int           { return len(sv) }
func (sv sortVts) Swap(i, j int)      { sv[j], sv[i] = sv[i], sv[j] }

func SortVts(vts []*Vtx) { sort.Sort(sortVts(vts)) }

type TestTx struct {
	snowstorm.TestTx
	bytes []byte
}

func (tx *TestTx) Bytes() []byte { return tx.bytes }

func Matches(a, b []ids.ID) bool {
	if len(a) != len(b) {
		return false
	}
	set := ids.Set{}
	set.Add(a...)
	for _, id := range b {
		if !set.Contains(id) {
			return false
		}
	}
	return true
}
func MatchesShort(a, b []ids.ShortID) bool {
	if len(a) != len(b) {
		return false
	}
	set := ids.ShortSet{}
	set.Add(a...)
	for _, id := range b {
		if !set.Contains(id) {
			return false
		}
	}
	return true
}
