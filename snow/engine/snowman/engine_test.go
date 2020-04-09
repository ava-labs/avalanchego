// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"sort"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
)

var (
	Genesis = GenerateID()
	offset  = uint64(0)
)

func GenerateID() ids.ID {
	offset++
	return ids.Empty.Prefix(offset)
}

type Blk struct {
	parent snowman.Block
	id     ids.ID

	height   int
	status   choices.Status
	validity error

	bytes []byte
}

func (b *Blk) ID() ids.ID             { return b.id }
func (b *Blk) Parent() snowman.Block  { return b.parent }
func (b *Blk) Accept()                { b.status = choices.Accepted }
func (b *Blk) Reject()                { b.status = choices.Rejected }
func (b *Blk) Status() choices.Status { return b.status }
func (b *Blk) Verify() error          { return b.validity }
func (b *Blk) Bytes() []byte          { return b.bytes }

type sortBks []*Blk

func (sb sortBks) Less(i, j int) bool { return sb[i].height < sb[j].height }
func (sb sortBks) Len() int           { return len(sb) }
func (sb sortBks) Swap(i, j int)      { sb[j], sb[i] = sb[i], sb[j] }

func SortBks(bks []*Blk) { sort.Sort(sortBks(bks)) }

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
