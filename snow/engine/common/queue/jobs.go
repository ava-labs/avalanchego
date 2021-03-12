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
	ErrDuplicate = errors.New("duplicated container")
)

// Jobs ...
type Jobs struct {
	parser Parser
	baseDB database.Database
	db     *versiondb.Database
	// Dynamic sized stack of ready to execute items
	// Map from itemID to list of itemIDs that are blocked on this item
	state                           prefixedState
	pending                         ids.Set
	removeFromPending, addToPending ids.Set
}

// New ...
func New(db database.Database) (*Jobs, error) {
	jobs := &Jobs{
		baseDB: db,
		db:     versiondb.New(db),
	}
	jobs.state.jobs = jobs
	pending, err := jobs.state.Pending(jobs.db)
	if err != nil {
		return nil, err
	}
	jobs.pending.Add(pending...)

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
func (j *Jobs) Commit() error {
	// Batch pending jobs into Commit to avoid making an unnecessary number
	// of put/deletes in the rapidly changing set.
	if err := j.updatePending(); err != nil {
		return err
	}
	return j.db.Commit()
}

// AddPendingID adds [jobID] to pending
func (j *Jobs) AddPendingID(jobIDs ...ids.ID) {
	for _, jobID := range jobIDs {
		if !j.pending.Contains(jobID) {
			j.pending.Add(jobID)
			j.addToPending.Add(jobID)
			j.removeFromPending.Remove(jobID)
		}
	}
}

// RemovePendingID removes [jobID] from pending
func (j *Jobs) RemovePendingID(jobIDs ...ids.ID) {
	for _, jobID := range jobIDs {
		if j.pending.Contains(jobID) {
			j.pending.Remove(jobID)
			j.addToPending.Remove(jobID)
			j.removeFromPending.Add(jobID)
		}
	}
}

func (j *Jobs) Pending() []ids.ID {
	return j.pending.List()
}

func (j *Jobs) PendingJobs() int { return j.pending.Len() }

// updatePending updates the set of pending jobs
func (j *Jobs) updatePending() error {
	// If there are no updates to be made return immediately
	if j.addToPending.Len()+j.removeFromPending.Len() == 0 {
		return nil
	}

	errs := wrappers.Errs{}
	errs.Add(
		j.state.AddPending(j.db, j.addToPending),
		j.state.RemovePending(j.db, j.removeFromPending),
	)

	// Clear the change sets once they've been written through
	// to the database.
	j.addToPending.Clear()
	j.removeFromPending.Clear()

	return errs.Err
}

func (j *Jobs) push(job Job) error {
	if has, err := j.state.HasJob(j.db, job.ID()); err != nil {
		return err
	} else if has {
		return ErrDuplicate
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
		return ErrDuplicate
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
