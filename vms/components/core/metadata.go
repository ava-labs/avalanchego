// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

// Metadata contains the data common to all blocks and transactions
type Metadata struct {
	id     ids.ID
	status choices.Status
	bytes  []byte
}

// ID returns the ID of this block/transaction
func (i *Metadata) ID() ids.ID { return i.id }

// Status returns the status of this block/transaction
func (i *Metadata) Status() choices.Status { return i.status }

// Bytes returns the byte repr. of this block/transaction
func (i *Metadata) Bytes() []byte { return i.bytes }

// SetStatus sets the status of this block/transaction
func (i *Metadata) SetStatus(status choices.Status) { i.status = status }

// Initialize sets [i.bytes] to [bytes], sets [i.id] to a hash of [i.bytes]
// and sets [i.status] to choices.Processing
func (i *Metadata) Initialize(bytes []byte) {
	i.bytes = bytes
	i.id = ids.NewID(hashing.ComputeHash256Array(i.bytes))
	i.status = choices.Processing
}
