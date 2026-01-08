// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
)

const progressUpdateFrequency = 30 * time.Second

// Jobs tracks a series of jobs that form a DAG of dependencies.
type Jobs struct {
	// db ensures that database updates are atomically updated.
	db *versiondb.Database
	// state writes the job queue to [db].
	state *state
	// Measures the ETA until bootstrapping finishes in nanoseconds.
	etaMetric prometheus.Gauge
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

	etaMetric := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNamespace,
		Name:      "eta_execution_complete",
		Help:      "ETA in nanoseconds until execution phase of bootstrapping finishes",
	})

	return &Jobs{
		db:        vdb,
		state:     state,
		etaMetric: etaMetric,
	}, metricsRegisterer.Register(etaMetric)
}

// SetParser tells this job queue how to parse jobs from the database.
func (j *Jobs) SetParser(parser Parser) error {
	j.state.parser = parser
	return nil
}

func (j *Jobs) Has(jobID ids.ID) (bool, error) {
	return j.state.HasJob(jobID)
}

// Returns how many pending jobs are waiting in the queue.
func (j *Jobs) PendingJobs() uint64 {
	return j.state.numJobs
}

// Push adds a new job to the queue. Returns true if [job] was added to the queue and false
// if [job] was already in the queue.
func (j *Jobs) Push(ctx context.Context, job Job) (bool, error) {
	jobID := job.ID()
	if has, err := j.state.HasJob(jobID); err != nil {
		return false, fmt.Errorf("failed to check for existing job %s due to %w", jobID, err)
	} else if has {
		return false, nil
	}

	deps, err := job.MissingDependencies(ctx)
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

func (j *Jobs) ExecuteAll(
	ctx context.Context,
	chainCtx *snow.ConsensusContext,
	halter common.Haltable,
	restarted bool,
	acceptors ...snow.Acceptor,
) (int, error) {
	chainCtx.Executing.Set(true)
	defer chainCtx.Executing.Set(false)

	numExecuted := 0
	numToExecute := j.state.numJobs
	startTime := time.Now()
	lastProgressUpdate := startTime

	// Disable and clear state caches to prevent us from attempting to execute
	// a vertex that was previously parsed, but not saved to the VM. Some VMs
	// may only persist containers when they are accepted. This is a stop-gap
	// measure to ensure the job will be re-parsed before executing until the VM
	// provides a more explicit interface for freeing parsed blocks.
	// TODO remove DisableCaching when VM provides better interface for freeing
	// blocks.
	j.state.DisableCaching()
	for {
		if halter.Halted() {
			chainCtx.Log.Info("interrupted execution",
				zap.Int("numExecuted", numExecuted),
			)
			return numExecuted, nil
		}

		job, err := j.state.RemoveRunnableJob(ctx)
		if err == database.ErrNotFound {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("failed to removing runnable job with %w", err)
		}

		jobID := job.ID()
		chainCtx.Log.Debug("executing",
			zap.Stringer("jobID", jobID),
		)
		jobBytes := job.Bytes()
		// Note that acceptor.Accept must be called before executing [job] to
		// honor Acceptor.Accept's invariant.
		for _, acceptor := range acceptors {
			if err := acceptor.Accept(chainCtx, jobID, jobBytes); err != nil {
				return numExecuted, err
			}
		}
		if err := job.Execute(ctx); err != nil {
			return 0, fmt.Errorf("failed to execute job %s due to %w", jobID, err)
		}

		dependentIDs, err := j.state.RemoveDependencies(jobID)
		if err != nil {
			return 0, fmt.Errorf("failed to remove blocking jobs for %s due to %w", jobID, err)
		}

		for _, dependentID := range dependentIDs {
			job, err := j.state.GetJob(ctx, dependentID)
			if err != nil {
				return 0, fmt.Errorf("failed to get job %s from blocking jobs due to %w", dependentID, err)
			}
			hasMissingDeps, err := job.HasMissingDependencies(ctx)
			if err != nil {
				return 0, fmt.Errorf("failed to get missing dependencies for %s due to %w", dependentID, err)
			}
			if hasMissingDeps {
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
		if time.Since(lastProgressUpdate) > progressUpdateFrequency { // Periodically print progress
			eta := timer.EstimateETA(
				startTime,
				uint64(numExecuted),
				numToExecute,
			)
			j.etaMetric.Set(float64(eta))

			if !restarted {
				chainCtx.Log.Info("executing operations",
					zap.Int("numExecuted", numExecuted),
					zap.Uint64("numToExecute", numToExecute),
					zap.Duration("eta", eta),
				)
			} else {
				chainCtx.Log.Debug("executing operations",
					zap.Int("numExecuted", numExecuted),
					zap.Uint64("numToExecute", numToExecute),
					zap.Duration("eta", eta),
				)
			}

			lastProgressUpdate = time.Now()
		}
	}

	// Now that executing has finished, zero out the ETA.
	j.etaMetric.Set(0)

	if !restarted {
		chainCtx.Log.Info("executed operations",
			zap.Int("numExecuted", numExecuted),
		)
	} else {
		chainCtx.Log.Debug("executed operations",
			zap.Int("numExecuted", numExecuted),
		)
	}
	return numExecuted, nil
}

func (j *Jobs) Clear() error {
	return j.state.Clear()
}

// Commit the versionDB to the underlying database.
func (j *Jobs) Commit() error {
	return j.db.Commit()
}

type JobsWithMissing struct {
	*Jobs

	// keep the missing ID set in memory to avoid unnecessary database reads and
	// writes.
	missingIDs                            set.Set[ids.ID]
	removeFromMissingIDs, addToMissingIDs set.Set[ids.ID]
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
func (jm *JobsWithMissing) SetParser(ctx context.Context, parser Parser) error {
	jm.state.parser = parser
	return jm.cleanRunnableStack(ctx)
}

func (jm *JobsWithMissing) Clear() error {
	if err := jm.state.RemoveMissingJobIDs(jm.missingIDs); err != nil {
		return err
	}

	jm.missingIDs.Clear()
	jm.addToMissingIDs.Clear()
	jm.removeFromMissingIDs.Clear()

	return jm.Jobs.Clear()
}

func (jm *JobsWithMissing) Has(jobID ids.ID) (bool, error) {
	if jm.missingIDs.Contains(jobID) {
		return false, nil
	}

	return jm.Jobs.Has(jobID)
}

// Push adds a new job to the queue. Returns true if [job] was added to the queue and false
// if [job] was already in the queue.
func (jm *JobsWithMissing) Push(ctx context.Context, job Job) (bool, error) {
	jobID := job.ID()
	if has, err := jm.Has(jobID); err != nil {
		return false, fmt.Errorf("failed to check for existing job %s due to %w", jobID, err)
	} else if has {
		return false, nil
	}

	deps, err := job.MissingDependencies(ctx)
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

func (jm *JobsWithMissing) MissingIDs() []ids.ID {
	return jm.missingIDs.List()
}

func (jm *JobsWithMissing) NumMissingIDs() int {
	return jm.missingIDs.Len()
}

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
func (jm *JobsWithMissing) cleanRunnableStack(ctx context.Context) error {
	runnableJobsIter := jm.state.runnableJobIDs.NewIterator()
	defer runnableJobsIter.Release()

	for runnableJobsIter.Next() {
		jobIDBytes := runnableJobsIter.Key()
		jobID, err := ids.ToID(jobIDBytes)
		if err != nil {
			return fmt.Errorf("failed to convert jobID bytes into ID due to: %w", err)
		}

		job, err := jm.state.GetJob(ctx, jobID)
		if err != nil {
			return fmt.Errorf("failed to retrieve job on runnable stack due to: %w", err)
		}
		deps, err := job.MissingDependencies(ctx)
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

	return errors.Join(
		runnableJobsIter.Error(),
		jm.Commit(),
	)
}
