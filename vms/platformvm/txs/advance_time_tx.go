// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var _ UnsignedTx = (*AdvanceTimeTx)(nil)

// AdvanceTimeTx is a transaction to increase the chain's timestamp.
// When the chain's timestamp is updated (a AdvanceTimeTx is accepted and
// followed by a commit block) the staker set is also updated accordingly.
// It must be that:
// - proposed timestamp > [current chain time]
// - proposed timestamp <= [time for next staker set change]
type AdvanceTimeTx struct {
	// Unix time this block proposes increasing the timestamp to
	Time uint64 `serialize:"true" json:"time"`

	unsignedBytes []byte // Unsigned byte representation of this data
}

func (tx *AdvanceTimeTx) SetBytes(unsignedBytes []byte) {
	tx.unsignedBytes = unsignedBytes
}

func (tx *AdvanceTimeTx) Bytes() []byte {
	return tx.unsignedBytes
}

func (*AdvanceTimeTx) InitCtx(*snow.Context) {}

// Timestamp returns the time this block is proposing the chain should be set to
func (tx *AdvanceTimeTx) Timestamp() time.Time {
	return time.Unix(int64(tx.Time), 0)
}

func (*AdvanceTimeTx) InputIDs() set.Set[ids.ID] {
	return nil
}

func (*AdvanceTimeTx) Outputs() []*avax.TransferableOutput {
	return nil
}

func (*AdvanceTimeTx) SyntacticVerify(*snow.Context) error {
	return nil
}

func (tx *AdvanceTimeTx) Visit(visitor Visitor) error {
	return visitor.AdvanceTimeTx(tx)
}
