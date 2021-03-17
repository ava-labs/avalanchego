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
	pendingID
)

var (
	stackSize     = []byte{stackSizeID}
	pendingPrefix = []byte{pendingID}
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

func (ps *prefixedState) DeleteStackSize(db database.Database) error {
	return ps.state.DeleteInt(db, stackSize)
}

// SetStackIndex sets the job at [index] to [job]
func (ps *prefixedState) SetStackIndex(db database.Database, index uint32, jobID ids.ID) error {
	p := wrappers.Packer{Bytes: make([]byte, 1+wrappers.IntLen)}

	p.PackByte(stackID)
	p.PackInt(index)

	return ps.state.SetJobID(db, p.Bytes, jobID)
}

// RemoveStackIndex fetches and deletes the job at [index] from [db]
func (ps *prefixedState) RemoveStackIndex(db database.Database, index uint32) (Job, error) {
	p := wrappers.Packer{Bytes: make([]byte, 1+wrappers.IntLen)}

	p.PackByte(stackID)
	p.PackInt(index)

	jobID, err := ps.state.JobID(db, p.Bytes)
	if err != nil {
		return nil, err
	}

	job, err := ps.Job(db, jobID)
	if err != nil {
		return nil, err
	}
	return job, db.Delete(p.Bytes)
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

// Blocking returns the set of IDs that are blocking on the completion of [id]
// and removes them from the database.
func (ps *prefixedState) RemoveBlocking(db database.Database, id ids.ID) ([]ids.ID, error) {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(blockingID)
	p.PackFixedBytes(id[:])

	blocking, err := ps.state.IDs(db, p.Bytes)
	if err != nil {
		return nil, err
	}
	for _, blocked := range blocking {
		if err := ps.state.RemoveID(db, p.Bytes, blocked); err != nil {
			return nil, err
		}
	}
	return blocking, nil
}

func (ps *prefixedState) AddPending(db database.Database, pendingIDs ids.Set) error {
	return ps.state.AddIDs(db, pendingPrefix, pendingIDs)
}

func (ps *prefixedState) RemovePending(db database.Database, pendingIDs ids.Set) error {
	return ps.state.RemoveIDs(db, pendingPrefix, pendingIDs)
}

func (ps *prefixedState) Pending(db database.Database) ([]ids.ID, error) {
	return ps.state.IDs(db, pendingPrefix)
}
