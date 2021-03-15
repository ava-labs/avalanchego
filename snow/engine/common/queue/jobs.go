// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/snow"
)

const (
	// StatusUpdateFrequency is how many containers should be processed between
	// logs
	StatusUpdateFrequency = 2500
)

var (
	errDuplicate = errors.New("duplicated container")
)

// Jobs tracks a series of jobs that form a DAG of dependencies.
type Jobs struct {
	// baseDB is the DB that we are flushing data to.
	baseDB database.Database
	// db ensures that [baseDB] is atomically updated.
	db *versiondb.Database
	// state prefixes different values to avoid namespace collisions.
	state prefixedState
	// keeps the stackSize in memory to avoid unnecessary database reads.
	stackSize uint32
}

// New attempts to create a new job queue from the provided database.
func New(db database.Database) (*Jobs, error) {
	jobs := &Jobs{
		baseDB: db,
		db:     versiondb.New(db),
	}

	stackSize, err := jobs.state.StackSize(jobs.db)
	if err == nil {
		jobs.stackSize = stackSize
		return jobs, nil
	}
	if err != database.ErrNotFound {
		return nil, err
	}
	return jobs, jobs.state.SetStackSize(jobs.db, 0)
}

// SetParser tells this job queue how to parse jobs from the database.
func (j *Jobs) SetParser(parser Parser) { j.state.parser = parser }

func (j *Jobs) Has(job Job) (bool, error) {
	return j.state.HasJob(j.db, job.ID())
}

// Push adds a new job to the queue.
func (j *Jobs) Push(job Job) error {
	deps, err := job.MissingDependencies()
	if err != nil {
		return err
	}
	jobID := job.ID()
	// Store this job into the database.
	if has, err := j.state.HasJob(j.db, jobID); err != nil {
		return fmt.Errorf("failed to check for existing job %s due to %w", jobID, err)
	} else if has {
		return errDuplicate
	}
	if err := j.state.SetJob(j.db, job); err != nil {
		return fmt.Errorf("failed to write job due to %w", err)
	}

	if deps.Len() != 0 {
		// This job needs to block on a set of dependencies.
		for depID := range deps {
			if err := j.state.AddBlocking(j.db, depID, jobID); err != nil {
				return fmt.Errorf("failed to add blocking for depID %s, jobID %s", depID, jobID)
			}
		}
		return nil
	}
	// This job doesn't have any dependencies, so it should be placed onto the
	// executable stack.
	return j.pushUnblockedJob(job)
}

// Pop removes a job from the queue, if there are no jobs left on the queue, a
// [database.ErrNotFound] error will be returned.
func (j *Jobs) Pop() (Job, error) {
	if j.stackSize == 0 {
		return nil, database.ErrNotFound
	}

	j.stackSize--
	if err := j.state.SetStackSize(j.db, j.stackSize); err != nil {
		return nil, fmt.Errorf("failed to set stack size due to %w", err)
	}

	job, err := j.state.RemoveStackIndex(j.db, j.stackSize)
	if err != nil {
		return nil, fmt.Errorf("failed to remove stack index due to %w", err)
	}
	return job, nil
}

// Execute takes a job that is ready to execute, and executes it. After
// execution, if there are any jobs that were blocking on this job, unblock
// those jobs.
func (j *Jobs) Execute(job Job) error {
	jobID := job.ID()
	if err := job.Execute(); err != nil {
		return fmt.Errorf("failed to execute job %s due to %w", jobID, err)
	}

	blocking, err := j.state.RemoveBlocking(j.db, jobID)
	if err != nil {
		return fmt.Errorf("failed to remove blocking jobs for %s due to %w", jobID, err)
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
	if err := j.state.DeleteJob(j.db, jobID); err != nil {
		return fmt.Errorf("failed to delete job %s due to %w", jobID, err)
	}
	return nil
}

func (j *Jobs) ExecuteAll(ctx *snow.Context, events ...snow.EventDispatcher) (int, error) {
	numExecuted := 0
	for j.stackSize > 0 {
		job, err := j.Pop()
		if err != nil {
			return 0, err
		}

		ctx.Log.Debug("Executing: %s", job.ID())
		if err := j.Execute(job); err != nil {
			return 0, err
		}
		if err := j.Commit(); err != nil {
			return 0, err
		}

		numExecuted++
		if numExecuted%StatusUpdateFrequency == 0 { // Periodically print progress
			ctx.Log.Info("executed %d operations", numExecuted)
		}

		for _, event := range events {
			event.Accept(ctx, job.ID(), job.Bytes())
		}
	}

	ctx.Log.Info("executed %d operations", numExecuted)
	return numExecuted, nil
}

// Commit the versionDB to the underlying database.
func (j *Jobs) Commit() error { return j.db.Commit() }

// pushUnblockedJob pushes a job with no remaining dependencies to the queue to
// be executed
func (j *Jobs) pushUnblockedJob(job Job) error {
	if err := j.state.SetStackIndex(j.db, j.stackSize, job.ID()); err != nil {
		return fmt.Errorf("failed to set stack index due to %w", err)
	}
	j.stackSize++
	if err := j.state.SetStackSize(j.db, j.stackSize); err != nil {
		return fmt.Errorf("failed to set stack size due to %w", err)
	}
	return nil
}
