// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
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
func New(
	db database.Database,
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) (*Jobs, error) {
	vdb := versiondb.New(db)
	state, err := newState(vdb, metricsNamespace, metricsRegisterer)
	if err != nil {
		return nil, fmt.Errorf("couldn't create new jobs state: %w", err)
	}

	return &Jobs{
		db:    vdb,
		state: state,
	}, nil
}

// SetParser tells this job queue how to parse jobs from the database.
func (j *Jobs) SetParser(parser Parser) error { j.state.parser = parser; return nil }

func (j *Jobs) Has(jobID ids.ID) (bool, error) { return j.state.HasJob(jobID) }

// Returns how many pending jobs are waiting in the queue.
func (j *Jobs) PendingJobs() uint64 { return j.state.numPendingJobs }

// Push adds a new job to the queue. Returns true if [job] was added to the queue and false
// if [job] was already in the queue.
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

func (j *Jobs) ExecuteAll(ctx *snow.ConsensusContext, halter common.Haltable,
	restarted bool, events ...snow.EventDispatcher) (int, error) {
	jExec := &jobExecutor{
		ctx:    ctx,
		events: events,
	}
	return j.loop(jExec.process, ctx, halter, restarted)
}

func (j *Jobs) ClearAll(ctx *snow.ConsensusContext, halter common.Haltable,
	restarted bool, events ...snow.EventDispatcher) (int, error) {
	jDrop := &jobDropper{}
	return j.loop(jDrop.process, ctx, halter, restarted)
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

func NewWithMissing(
	db database.Database,
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) (*JobsWithMissing, error) {
	innerJobs, err := New(db, metricsNamespace, metricsRegisterer)
	if err != nil {
		return nil, err
	}

	jobs := &JobsWithMissing{
		Jobs: innerJobs,
	}

	missingIDs, err := jobs.state.MissingJobIDs()
	jobs.missingIDs.Add(missingIDs...)
	return jobs, err
}

// SetParser tells this job queue how to parse jobs from the database.
func (jm *JobsWithMissing) SetParser(parser Parser) error {
	jm.state.parser = parser
	return jm.cleanRunnableStack()
}

func (jm *JobsWithMissing) ClearAll(ctx *snow.ConsensusContext, halter common.Haltable,
	restarted bool, events ...snow.EventDispatcher) (int, error) {
	clearedJobs := 0
	if err := jm.state.RemoveMissingJobIDs(jm.missingIDs); err != nil {
		return clearedJobs, err
	}

	clearedJobs = jm.missingIDs.Len()
	jm.missingIDs.Clear()
	jm.addToMissingIDs.Clear()
	jm.removeFromMissingIDs.Clear()

	clearedInnerJobs, err := jm.Jobs.ClearAll(ctx, halter, restarted, events...)
	return clearedJobs + clearedInnerJobs, err
}

func (jm *JobsWithMissing) Has(jobID ids.ID) (bool, error) {
	if jm.missingIDs.Contains(jobID) {
		return false, nil
	}

	return jm.Jobs.Has(jobID)
}

// Push adds a new job to the queue. Returns true if [job] was added to the queue and false
// if [job] was already in the queue.
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

// cleanRunnableStack iterates over the jobs on the runnable stack and resets any job
// that has missing dependencies to block on those dependencies.
// Note: the jobs queue ensures that no job with missing dependencies will be placed
// on the runnable stack in the first place.
// However, for specific VM implementations blocks may be committed via a two stage commit
// (ex. platformvm Proposal and Commit/Abort blocks). This can cause an issue where if the first stage
// is executed immediately before the node dies, it will be removed from the runnable stack
// without writing the state transition to the VM's database. When the node restarts, the
// VM will not have marked the first block (the proposal block as accepted), but it could
// have already been removed from the jobs queue. cleanRunnableStack handles this case.
func (jm *JobsWithMissing) cleanRunnableStack() error {
	runnableJobsIter := jm.state.runnableJobIDs.NewIterator()
	defer runnableJobsIter.Release()

	for runnableJobsIter.Next() {
		jobIDBytes := runnableJobsIter.Key()
		jobID, err := ids.ToID(jobIDBytes)
		if err != nil {
			return fmt.Errorf("failed to convert jobID bytes into ID due to: %w", err)
		}

		job, err := jm.state.GetJob(jobID)
		if err != nil {
			return fmt.Errorf("failed to retrieve job on runnnable stack due to: %w", err)
		}
		deps, err := job.MissingDependencies()
		if err != nil {
			return fmt.Errorf("failed to retrieve missing dependencies of job on runnable stack due to: %w", err)
		}
		if deps.Len() == 0 {
			continue
		}

		// If the job has missing dependencies, remove it from the runnable stack
		if err := jm.state.runnableJobIDs.Delete(jobIDBytes); err != nil {
			return fmt.Errorf("failed to delete jobID from runnable stack due to: %w", err)
		}

		// Add the missing dependencies to the set that needs to be fetched.
		jm.AddMissingID(deps.List()...)
		for depID := range deps {
			if err := jm.state.AddDependency(depID, jobID); err != nil {
				return fmt.Errorf("failed to add blocking for depID %s, jobID %s while cleaning the runnable stack", depID, jobID)
			}
		}
	}

	errs := wrappers.Errs{}
	errs.Add(
		runnableJobsIter.Error(),
		jm.Commit(),
	)
	return errs.Err
}
