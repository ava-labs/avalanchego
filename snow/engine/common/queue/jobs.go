// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
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
	// db ensures that database updates are atomically updated.
	db *versiondb.Database
	// state writes the job queue to [db].
	state *state
	// keep the missing ID set in memory to avoid unnecessary database reads and
	// writes.
	missingIDs                            ids.Set
	removeFromMissingIDs, addToMissingIDs ids.Set
}

// New attempts to create a new job queue from the provided database.
func New(db database.Database) (*Jobs, error) {
	vdb := versiondb.New(db)
	jobs := &Jobs{
		db:    vdb,
		state: newState(vdb),
	}

	missingIDs, err := jobs.state.MissingJobIDs()
	jobs.missingIDs.Add(missingIDs...)
	return jobs, err
}

// SetParser tells this job queue how to parse jobs from the database.
func (j *Jobs) SetParser(parser Parser) { j.state.parser = parser }

func (j *Jobs) Has(jobID ids.ID) (bool, error) { return j.state.HasJob(jobID) }

// Push adds a new job to the queue.
func (j *Jobs) Push(job Job) error {
	deps, err := job.MissingDependencies()
	if err != nil {
		return err
	}
	jobID := job.ID()
	// Store this job into the database.
	if has, err := j.state.HasJob(jobID); err != nil {
		return fmt.Errorf("failed to check for existing job %s due to %w", jobID, err)
	} else if has {
		return errDuplicate
	}
	if err := j.state.PutJob(job); err != nil {
		return fmt.Errorf("failed to write job due to %w", err)
	}

	if deps.Len() != 0 {
		// This job needs to block on a set of dependencies.
		for depID := range deps {
			if err := j.state.AddDependency(depID, jobID); err != nil {
				return fmt.Errorf("failed to add blocking for depID %s, jobID %s", depID, jobID)
			}
		}
		return nil
	}
	// This job doesn't have any dependencies, so it should be placed onto the
	// executable stack.
	if err := j.state.AddRunnableJob(jobID); err != nil {
		return fmt.Errorf("failed to add %s as a runnable job due to %w", jobID, err)
	}
	return nil
}

// AddMissingID adds [jobID] to missingIDs
func (j *Jobs) AddMissingID(jobIDs ...ids.ID) {
	for _, jobID := range jobIDs {
		if !j.missingIDs.Contains(jobID) {
			j.missingIDs.Add(jobID)
			j.addToMissingIDs.Add(jobID)
			j.removeFromMissingIDs.Remove(jobID)
		}
	}
}

// RemoveMissingID removes [jobID] from missingIDs
func (j *Jobs) RemoveMissingID(jobIDs ...ids.ID) {
	for _, jobID := range jobIDs {
		if j.missingIDs.Contains(jobID) {
			j.missingIDs.Remove(jobID)
			j.addToMissingIDs.Remove(jobID)
			j.removeFromMissingIDs.Add(jobID)
		}
	}
}

func (j *Jobs) MissingIDs() []ids.ID { return j.missingIDs.List() }

func (j *Jobs) NumMissingIDs() int { return j.missingIDs.Len() }

func (j *Jobs) ExecuteAll(ctx *snow.Context, restarted bool, events ...snow.EventDispatcher) (int, error) {
	numExecuted := 0
	for {
		job, err := j.state.RemoveRunnableJob()
		if err == database.ErrNotFound {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("failed to removing runnable job with %w", err)
		}

		jobID := job.ID()
		ctx.Log.Debug("Executing: %s", jobID)
		if err := job.Execute(); err != nil {
			return 0, fmt.Errorf("failed to execute job %s due to %w", jobID, err)
		}

		dependentIDs, err := j.state.RemoveDependencies(jobID)
		if err != nil {
			return 0, fmt.Errorf("failed to remove blocking jobs for %s due to %w", jobID, err)
		}

		for _, dependentID := range dependentIDs {
			job, err := j.state.GetJob(dependentID)
			if err != nil {
				return 0, fmt.Errorf("failed to get job %s from blocking jobs due to %w", dependentID, err)
			}
			deps, err := job.MissingDependencies()
			if err != nil {
				return 0, fmt.Errorf("failed to get missing dependencies for %s due to %w", dependentID, err)
			}
			if deps.Len() > 0 {
				continue
			}
			if err := j.state.AddRunnableJob(dependentID); err != nil {
				return 0, fmt.Errorf("failed to add %s as a runnable job due to %w", dependentID, err)
			}
		}
		if err := j.Commit(); err != nil {
			return 0, err
		}

		numExecuted++
		if numExecuted%StatusUpdateFrequency == 0 { // Periodically print progress
			if !restarted {
				ctx.Log.Info("executed %d operations", numExecuted)
			} else {
				ctx.Log.Debug("executed %d operations", numExecuted)
			}
		}

		// TODO putting this after commit could cause dropped events
		// Ex. if a job is executed and accepted, committed to the database, and then
		// the node dies, then since the event has been committed we will not execute
		// it again and therefore the event will never be sent.
		// We should be able to fix this at the risk of emitting duplicate events by
		// sending Accept events before the job has been committed. Not sure which is
		// better here.
		for _, event := range events {
			if err := event.Accept(ctx, job.ID(), job.Bytes()); err != nil {
				return numExecuted, err
			}
		}
	}

	if !restarted {
		ctx.Log.Info("executed %d operations", numExecuted)
	} else {
		ctx.Log.Debug("executed %d operations", numExecuted)
	}
	return numExecuted, nil
}

// Commit the versionDB to the underlying database.
func (j *Jobs) Commit() error {
	if j.addToMissingIDs.Len() != 0 {
		if err := j.state.AddMissingJobIDs(j.addToMissingIDs); err != nil {
			return err
		}
		j.addToMissingIDs.Clear()
	}
	if j.removeFromMissingIDs.Len() != 0 {
		if err := j.state.RemoveMissingJobIDs(j.removeFromMissingIDs); err != nil {
			return err
		}
		j.removeFromMissingIDs.Clear()
	}
	return j.db.Commit()
}
