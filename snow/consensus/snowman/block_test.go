// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"errors"
	"sort"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
)

type TestBlock struct {
	parent Block
	id     ids.ID
	height int
	status choices.Status
	bytes  []byte
	err    error
}

func (b *TestBlock) Parent() Block          { return b.parent }
func (b *TestBlock) ID() ids.ID             { return b.id }
func (b *TestBlock) Status() choices.Status { return b.status }
func (b *TestBlock) Accept() error {
	if b.status.Decided() && b.status != choices.Accepted {
		return errors.New("Dis-agreement")
	}
	b.status = choices.Accepted
	return b.err
}
func (b *TestBlock) Reject() error {
	if b.status.Decided() && b.status != choices.Rejected {
		return errors.New("Dis-agreement")
	}
	b.status = choices.Rejected
	return b.err
}
func (b *TestBlock) Verify() error { return b.err }
func (b *TestBlock) Bytes() []byte { return b.bytes }

type sortBlocks []*TestBlock

func (sb sortBlocks) Less(i, j int) bool { return sb[i].height < sb[j].height }
func (sb sortBlocks) Len() int           { return len(sb) }
func (sb sortBlocks) Swap(i, j int)      { sb[j], sb[i] = sb[i], sb[j] }

func SortVts(blocks []*TestBlock) { sort.Sort(sortBlocks(blocks)) }
