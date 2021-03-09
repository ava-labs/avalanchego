// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
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
	// keep [stackSize] in memory to avoid unnecessary database reads
	stackSize uint32
}

// New ...
func New(db database.Database) (*Jobs, error) {
	jobs := &Jobs{
		baseDB: db,
		db:     versiondb.New(db),
	}
	jobs.state.jobs = jobs

	stackSize, err := jobs.state.StackSize(jobs.db)
	if err == nil {
		jobs.stackSize = stackSize
		return jobs, nil
	}

	jobs.stackSize = 0
	if err := jobs.state.SetStackSize(jobs.db, jobs.stackSize); err != nil {
		return nil, err
	}
	return jobs, nil
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
	if j.stackSize == 0 {
		return nil, errEmpty
	}

	j.stackSize--
	if err := j.state.SetStackSize(j.db, j.stackSize); err != nil {
		j.stackSize++
		return nil, fmt.Errorf("failed to set stack size due to %w", err)
	}
	job, err := j.state.StackIndex(j.db, j.stackSize)
	if err != nil {
		return nil, fmt.Errorf("failed to read stack index due to %w", err)
	}
	err = j.state.DeleteStackIndex(j.db, j.stackSize)
	if err != nil {
		return nil, fmt.Errorf("failed to delete stack index due to %w", err)
	}
	return job, nil
}

// HasNext ...
func (j *Jobs) HasNext() (bool, error) {
	return j.stackSize > 0, nil
}

// Execute ...
func (j *Jobs) Execute(job Job) error {
	jobID := job.ID()

	if err := job.Execute(); err != nil {
		return fmt.Errorf("failed to execute job %s due to %w", job.ID(), err)
	}

	blocking, err := j.state.Blocking(j.db, jobID)
	if err != nil {
		return fmt.Errorf("failed to retrieve blocking jobs for %s due to %w", jobID, err)
	}
	if err := j.state.DeleteBlocking(j.db, jobID, blocking); err != nil {
		return fmt.Errorf("failed to delete blocking %w", err)
	}

	for _, blockedID := range blocking {
		job, err := j.state.Job(j.db, blockedID)
		if err != nil {
			return fmt.Errorf("failed to get job %s from blocking jobs due to %w", blockedID, err)
		}
		deps, err := job.MissingDependencies()
		if err != nil {
			return fmt.Errorf("failed to get missing dependencies for %s due to %w", job.ID(), err)
		}
		if deps.Len() > 0 {
			continue
		}
		if err := j.pushUnblockedJob(job); err != nil {
			return err
		}
	}

	err = j.state.DeleteJob(j.db, jobID)
	if err != nil {
		return fmt.Errorf("failed to delete job %s due to %w", jobID, err)
	}

	return err
}

// Commit ...
func (j *Jobs) Commit() error { return j.db.Commit() }

func (j *Jobs) push(job Job) error {
	if err := j.storeJob(job); err != nil {
		return err
	}

	return j.pushUnblockedJob(job)
}

// pushUnblockedJob pushes a new job with no remaining dependencies to the queue
// to be executed
func (j *Jobs) pushUnblockedJob(job Job) error {
	err := j.state.SetStackIndex(j.db, j.stackSize, job.ID())
	if err != nil {
		return fmt.Errorf("failed to set stack index due to %w", err)
	}
	j.stackSize++
	err = j.state.SetStackSize(j.db, j.stackSize)
	if err != nil {
		j.stackSize--
		return fmt.Errorf("failed to set stack size due to %w", err)
	}

	return nil
}

func (j *Jobs) block(job Job, deps ids.Set) error {
	if err := j.storeJob(job); err != nil {
		return err
	}

	jobID := job.ID()
	for depID := range deps {
		if err := j.state.AddBlocking(j.db, depID, jobID); err != nil {
			return fmt.Errorf("failed to add blocking for depID %s, jobID %s", depID, jobID)
		}
	}

	return nil
}

func (j *Jobs) storeJob(job Job) error {
	if has, err := j.state.HasJob(j.db, job.ID()); err != nil {
		return fmt.Errorf("failed to check for existing job %s due to %w", job.ID(), err)
	} else if has {
		return errDuplicate
	}

	if err := j.state.SetJob(j.db, job); err != nil {
		return fmt.Errorf("failed to write job due to %w", err)
	}

	return nil
}
