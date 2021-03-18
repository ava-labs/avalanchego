// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
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

// Jobs tracks a series of jobs that form a DAG of dependencies.
type Jobs struct {
	// db ensures that database updates are atomically updated.
	db *versiondb.Database
	// state writes the job queue to [db].
	state *state
}

// New attempts to create a new job queue from the provided database.
func New(db database.Database) *Jobs {
	vdb := versiondb.New(db)
	return &Jobs{
		db:    vdb,
		state: newState(vdb),
	}
}

// SetParser tells this job queue how to parse jobs from the database.
func (j *Jobs) SetParser(parser Parser) { j.state.parser = parser }

func (j *Jobs) Has(jobID ids.ID) (bool, error) { return j.state.HasJob(jobID) }

// Push adds a new job to the queue.
func (j *Jobs) Push(job Job) (bool, error) {
	jobID := job.ID()
	if has, err := j.state.HasJob(jobID); err != nil {
		return false, fmt.Errorf("failed to check for existing job %s due to %w", jobID, err)
	} else if has {
		return false, nil
	}

	deps, err := job.MissingDependencies()
	if err != nil {
		return false, err
	}
	// Store this job into the database.
	if err := j.state.PutJob(job); err != nil {
		return false, fmt.Errorf("failed to write job due to %w", err)
	}

	if deps.Len() != 0 {
		// This job needs to block on a set of dependencies.
		for depID := range deps {
			if err := j.state.AddDependency(depID, jobID); err != nil {
				return false, fmt.Errorf("failed to add blocking for depID %s, jobID %s", depID, jobID)
			}
		}
		return true, nil
	}
	// This job doesn't have any dependencies, so it should be placed onto the
	// executable stack.
	if err := j.state.AddRunnableJob(jobID); err != nil {
		return false, fmt.Errorf("failed to add %s as a runnable job due to %w", jobID, err)
	}
	return true, nil
}

func (j *Jobs) ExecuteAll(ctx *snow.Context, events ...snow.EventDispatcher) (int, error) {
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
func (j *Jobs) Commit() error {
	return j.db.Commit()
}

type JobsWithMissing struct {
	*Jobs

	// keep the missing ID set in memory to avoid unnecessary database reads and
	// writes.
	missingIDs                            ids.Set
	removeFromMissingIDs, addToMissingIDs ids.Set
}

func NewWithMissing(db database.Database) (*JobsWithMissing, error) {
	jobs := &JobsWithMissing{
		Jobs: New(db),
	}

	missingIDs, err := jobs.state.MissingJobIDs()
	jobs.missingIDs.Add(missingIDs...)
	return jobs, err
}

func (jm *JobsWithMissing) Has(jobID ids.ID) (bool, error) {
	if jm.missingIDs.Contains(jobID) {
		return false, nil
	}

	return jm.Jobs.Has(jobID)
}

// Push adds a new job to the queue.
func (jm *JobsWithMissing) Push(job Job) (bool, error) {
	jobID := job.ID()
	if has, err := jm.Has(jobID); err != nil {
		return false, fmt.Errorf("failed to check for existing job %s due to %w", jobID, err)
	} else if has {
		return false, nil
	}

	deps, err := job.MissingDependencies()
	if err != nil {
		return false, err
	}
	// Store this job into the database.
	if err := jm.state.PutJob(job); err != nil {
		return false, fmt.Errorf("failed to write job due to %w", err)
	}

	if deps.Len() != 0 {
		// This job needs to block on a set of dependencies.
		for depID := range deps {
			if err := jm.state.AddDependency(depID, jobID); err != nil {
				return false, fmt.Errorf("failed to add blocking for depID %s, jobID %s", depID, jobID)
			}
		}
		return true, nil
	}
	// This job doesn't have any dependencies, so it should be placed onto the
	// executable stack.
	if err := jm.state.AddRunnableJob(jobID); err != nil {
		return false, fmt.Errorf("failed to add %s as a runnable job due to %w", jobID, err)
	}
	return true, nil
}

// AddMissingID adds [jobID] to missingIDs
func (jm *JobsWithMissing) AddMissingID(jobIDs ...ids.ID) {
	for _, jobID := range jobIDs {
		if !jm.missingIDs.Contains(jobID) {
			jm.missingIDs.Add(jobID)
			jm.addToMissingIDs.Add(jobID)
			jm.removeFromMissingIDs.Remove(jobID)
		}
	}
}

// RemoveMissingID removes [jobID] from missingIDs
func (jm *JobsWithMissing) RemoveMissingID(jobIDs ...ids.ID) {
	for _, jobID := range jobIDs {
		if jm.missingIDs.Contains(jobID) {
			jm.missingIDs.Remove(jobID)
			jm.addToMissingIDs.Remove(jobID)
			jm.removeFromMissingIDs.Add(jobID)
		}
	}
}

func (jm *JobsWithMissing) MissingIDs() []ids.ID { return jm.missingIDs.List() }

func (jm *JobsWithMissing) NumMissingIDs() int { return jm.missingIDs.Len() }

// Commit the versionDB to the underlying database.
func (jm *JobsWithMissing) Commit() error {
	if jm.addToMissingIDs.Len() != 0 {
		if err := jm.state.AddMissingJobIDs(jm.addToMissingIDs); err != nil {
			return err
		}
		jm.addToMissingIDs.Clear()
	}
	if jm.removeFromMissingIDs.Len() != 0 {
		if err := jm.state.RemoveMissingJobIDs(jm.removeFromMissingIDs); err != nil {
			return err
		}
		jm.removeFromMissingIDs.Clear()
	}
	return jm.Jobs.Commit()
}
