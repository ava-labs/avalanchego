// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package missing

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

var errMissingBlock = errors.New("missing block")

// Block represents a block that can't be found
type Block struct{ BlkID ids.ID }

func (mb *Block) ID() ids.ID { return mb.BlkID }

func (mb *Block) Height() uint64 { return 0 }

func (mb *Block) Timestamp() time.Time { return time.Time{} }

func (*Block) Accept() error { return errMissingBlock }

func (*Block) Reject() error { return errMissingBlock }

func (*Block) Status() choices.Status { return choices.Unknown }

func (*Block) Parent() snowman.Block { return nil }

func (*Block) Verify() error { return errMissingBlock }

func (*Block) Bytes() []byte { return nil }
