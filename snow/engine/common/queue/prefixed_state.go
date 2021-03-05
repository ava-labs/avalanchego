// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

// Constants
const (
	stackSizeID byte = iota
	stackID
	jobID
	blockingID
)

var (
	stackSize = []byte{stackSizeID}
)

type prefixedState struct{ state }

// SetStackSize sets the size of the stack stored in [db] to [size]
func (ps *prefixedState) SetStackSize(db database.Database, size uint32) error {
	return ps.state.SetInt(db, stackSize, size)
}

// StackSize returns the size of the stack stored in [db]
func (ps *prefixedState) StackSize(db database.Database) (uint32, error) {
	return ps.state.Int(db, stackSize)
}

// SetStackIndex sets the job at [index] to [job]
func (ps *prefixedState) SetStackIndex(db database.Database, index uint32, job Job) error {
	p := wrappers.Packer{Bytes: make([]byte, 1+wrappers.IntLen)}

	p.PackByte(stackID)
	p.PackInt(index)

	return ps.state.SetJob(db, p.Bytes, job)
}

// DeleteStackIndex deletes the job at [index] from [db]
func (ps *prefixedState) DeleteStackIndex(db database.Database, index uint32) error {
	p := wrappers.Packer{Bytes: make([]byte, 1+wrappers.IntLen)}

	p.PackByte(stackID)
	p.PackInt(index)

	return db.Delete(p.Bytes)
}

// StackIndex returns the job at [index] in [db]
func (ps *prefixedState) StackIndex(db database.Database, index uint32) (Job, error) {
	p := wrappers.Packer{Bytes: make([]byte, 1+wrappers.IntLen)}

	p.PackByte(stackID)
	p.PackInt(index)

	return ps.state.Job(db, p.Bytes)
}

// SetJob writes [job] to [db]
func (ps *prefixedState) SetJob(db database.Database, job Job) error {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(jobID)
	id := job.ID()
	p.PackFixedBytes(id[:])

	return ps.state.SetJob(db, p.Bytes, job)
}

// HasJob returns true if [db] has job [id]
func (ps *prefixedState) HasJob(db database.Database, id ids.ID) (bool, error) {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(jobID)
	p.PackFixedBytes(id[:])

	return db.Has(p.Bytes)
}

// DeleteJob deletes job [id] from [db]
func (ps *prefixedState) DeleteJob(db database.Database, id ids.ID) error {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(jobID)
	p.PackFixedBytes(id[:])

	return db.Delete(p.Bytes)
}

// Job returns job [id] from [db]
func (ps *prefixedState) Job(db database.Database, id ids.ID) (Job, error) {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(jobID)
	p.PackFixedBytes(id[:])

	return ps.state.Job(db, p.Bytes)
}

// AddBlocking adds [blocking] as dependent on [id] being completed
func (ps *prefixedState) AddBlocking(db database.Database, id ids.ID, blocking ids.ID) error {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(blockingID)
	p.PackFixedBytes(id[:])

	return ps.state.AddID(db, p.Bytes, blocking)
}

// DeleteBlocking removes each item in [blocking] from the set of IDs that are blocked
// on the completion of [id]
func (ps *prefixedState) DeleteBlocking(db database.Database, id ids.ID, blocking []ids.ID) error {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(blockingID)
	p.PackFixedBytes(id[:])

	for _, blocked := range blocking {
		if err := ps.state.RemoveID(db, p.Bytes, blocked); err != nil {
			return err
		}
	}

	return nil
}

// Blocking returns the set of IDs that are blocking on the completion of [id]
func (ps *prefixedState) Blocking(db database.Database, id ids.ID) ([]ids.ID, error) {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(blockingID)
	p.PackFixedBytes(id[:])

	return ps.state.IDs(db, p.Bytes)
}
