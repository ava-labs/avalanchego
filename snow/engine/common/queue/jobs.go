// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"errors"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errEmpty     = errors.New("no available containers")
	errDuplicate = errors.New("duplicated container")
)

// Jobs ...
type Jobs struct {
	parser Parser
	baseDB database.Database
	db     *versiondb.Database
	// Dynamic sized stack of ready to execute items
	// Map from itemID to list of itemIDs that are blocked on this item
	state prefixedState
}

// New ...
func New(db database.Database) (*Jobs, error) {
	jobs := &Jobs{
		baseDB: db,
		db:     versiondb.New(db),
	}
	jobs.state.jobs = jobs

	if _, err := jobs.HasNext(); err == nil {
		return jobs, nil
	}
	return jobs, jobs.state.SetStackSize(jobs.db, 0)
}

// SetParser ...
func (j *Jobs) SetParser(parser Parser) { j.parser = parser }

// Push ...
func (j *Jobs) Push(job Job) error {
	deps, err := job.MissingDependencies()
	if err != nil {
		return err
	}
	if deps.Len() != 0 {
		return j.block(job, deps)
	}

	return j.push(job)
}

// Pop ...
func (j *Jobs) Pop() (Job, error) {
	size, err := j.state.StackSize(j.db)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, errEmpty
	}
	if err := j.state.SetStackSize(j.db, size-1); err != nil {
		return nil, err
	}
	job, err := j.state.StackIndex(j.db, size-1)
	if err != nil {
		return nil, err
	}
	return job, j.state.DeleteStackIndex(j.db, size-1)
}

// HasNext ...
func (j *Jobs) HasNext() (bool, error) {
	size, err := j.state.StackSize(j.db)
	return size > 0, err
}

// Execute ...
func (j *Jobs) Execute(job Job) error {
	if err := job.Execute(); err != nil {
		return err
	}

	jobID := job.ID()

	blocking, err := j.state.Blocking(j.db, jobID)
	if err != nil {
		return err
	}
	if err := j.state.DeleteBlocking(j.db, jobID, blocking); err != nil {
		return err
	}

	for _, blockedID := range blocking {
		job, err := j.state.Job(j.db, blockedID)
		if err != nil {
			return err
		}
		deps, err := job.MissingDependencies()
		if err != nil {
			return err
		}
		if deps.Len() > 0 {
			continue
		}
		// TODO: Calling execute here ensures that double decision blocks are
		//       always called atomically with their proposal blocks. This is
		//       only a quick fix and should be handled in a more robust manner.
		if err := job.Execute(); err != nil {
			return err
		}
		if err := j.state.DeleteJob(j.db, blockedID); err != nil {
			return err
		}
		if err := j.push(job); err != nil {
			return err
		}
	}

	return nil
}

// Commit ...
func (j *Jobs) Commit() error { return j.db.Commit() }

func (j *Jobs) push(job Job) error {
	if has, err := j.state.HasJob(j.db, job.ID()); err != nil {
		return err
	} else if has {
		return errDuplicate
	}

	if err := j.state.SetJob(j.db, job); err != nil {
		return err
	}

	errs := wrappers.Errs{}

	size, err := j.state.StackSize(j.db)
	errs.Add(err)
	errs.Add(j.state.SetStackIndex(j.db, size, job))
	errs.Add(j.state.SetStackSize(j.db, size+1))

	return errs.Err
}

func (j *Jobs) block(job Job, deps ids.Set) error {
	if has, err := j.state.HasJob(j.db, job.ID()); err != nil {
		return err
	} else if has {
		return errDuplicate
	}

	if err := j.state.SetJob(j.db, job); err != nil {
		return err
	}

	jobID := job.ID()
	for depID := range deps {
		if err := j.state.AddBlocking(j.db, depID, jobID); err != nil {
			return err
		}
	}

	return nil
}
