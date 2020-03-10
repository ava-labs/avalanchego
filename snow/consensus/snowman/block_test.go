// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"sort"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
)

type Blk struct {
	parent Block
	id     ids.ID
	height int
	status choices.Status
	bytes  []byte
}

func (b *Blk) Parent() Block          { return b.parent }
func (b *Blk) ID() ids.ID             { return b.id }
func (b *Blk) Status() choices.Status { return b.status }
func (b *Blk) Accept() {
	if b.status.Decided() && b.status != choices.Accepted {
		panic("Dis-agreement")
	}
	b.status = choices.Accepted
}
func (b *Blk) Reject() {
	if b.status.Decided() && b.status != choices.Rejected {
		panic("Dis-agreement")
	}
	b.status = choices.Rejected
}
func (b *Blk) Verify() error { return nil }
func (b *Blk) Bytes() []byte { return b.bytes }

type sortBlks []*Blk

func (sb sortBlks) Less(i, j int) bool { return sb[i].height < sb[j].height }
func (sb sortBlks) Len() int           { return len(sb) }
func (sb sortBlks) Swap(i, j int)      { sb[j], sb[i] = sb[i], sb[j] }

func SortVts(blks []*Blk) { sort.Sort(sortBlks(blks)) }
