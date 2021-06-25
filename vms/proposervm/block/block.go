// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type Block interface {
	ID() ids.ID

	ParentID() ids.ID
	PChainHeight() uint64
	Timestamp() time.Time
	Block() []byte
	Proposer() ids.ShortID

	Bytes() []byte

	Verify() error
}
