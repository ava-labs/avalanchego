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

func (ps *prefixedState) SetStackSize(db database.Database, size uint32) error {
	return ps.state.SetInt(db, stackSize, size)
}

func (ps *prefixedState) StackSize(db database.Database) (uint32, error) {
	return ps.state.Int(db, stackSize)
}

func (ps *prefixedState) SetStackIndex(db database.Database, index uint32, job Job) error {
	p := wrappers.Packer{Bytes: make([]byte, 1+wrappers.IntLen)}

	p.PackByte(stackID)
	p.PackInt(index)

	return ps.state.SetJob(db, p.Bytes, job)
}

func (ps *prefixedState) DeleteStackIndex(db database.Database, index uint32) error {
	p := wrappers.Packer{Bytes: make([]byte, 1+wrappers.IntLen)}

	p.PackByte(stackID)
	p.PackInt(index)

	return db.Delete(p.Bytes)
}

func (ps *prefixedState) StackIndex(db database.Database, index uint32) (Job, error) {
	p := wrappers.Packer{Bytes: make([]byte, 1+wrappers.IntLen)}

	p.PackByte(stackID)
	p.PackInt(index)

	return ps.state.Job(db, p.Bytes)
}

func (ps *prefixedState) SetJob(db database.Database, job Job) error {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(jobID)
	p.PackFixedBytes(job.ID().Bytes())

	return ps.state.SetJob(db, p.Bytes, job)
}

func (ps *prefixedState) HasJob(db database.Database, id ids.ID) (bool, error) {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(jobID)
	p.PackFixedBytes(id.Bytes())

	return db.Has(p.Bytes)
}

func (ps *prefixedState) DeleteJob(db database.Database, id ids.ID) error {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(jobID)
	p.PackFixedBytes(id.Bytes())

	return db.Delete(p.Bytes)
}

func (ps *prefixedState) Job(db database.Database, id ids.ID) (Job, error) {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(jobID)
	p.PackFixedBytes(id.Bytes())

	return ps.state.Job(db, p.Bytes)
}

func (ps *prefixedState) AddBlocking(db database.Database, id ids.ID, blocking ids.ID) error {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(blockingID)
	p.PackFixedBytes(id.Bytes())

	return ps.state.AddID(db, p.Bytes, blocking)
}

func (ps *prefixedState) DeleteBlocking(db database.Database, id ids.ID, blocking []ids.ID) error {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(blockingID)
	p.PackFixedBytes(id.Bytes())

	for _, blocked := range blocking {
		if err := ps.state.RemoveID(db, p.Bytes, blocked); err != nil {
			return err
		}
	}

	return nil
}

func (ps *prefixedState) Blocking(db database.Database, id ids.ID) ([]ids.ID, error) {
	p := wrappers.Packer{Bytes: make([]byte, 1+hashing.HashLen)}

	p.PackByte(blockingID)
	p.PackFixedBytes(id.Bytes())

	return ps.state.IDs(db, p.Bytes)
}
