// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// jobExecutor can be fed to jobs.loop to execute each job in the queue
type jobExecutor struct {
	ctx    *snow.ConsensusContext
	events []snow.EventDispatcher
}

func (je *jobExecutor) process(job Job) error {
	je.ctx.Log.Debug("Executing: %s", job.ID())
	// Note that event.Accept must be called before executing [job]
	// to honor EventDispatcher.Accept's invariant.
	for _, event := range je.events {
		if err := event.Accept(je.ctx, job.ID(), job.Bytes()); err != nil {
			return err
		}
	}

	if err := job.Execute(); err != nil {
		return fmt.Errorf("failed to execute job %s due to %w", job.ID(), err)
	}

	return nil
}

// jobDropper can be fed to jobs.loop to drop each job in the queue
type jobDropper struct{}

func (jd *jobDropper) process(job Job) error {
	return nil // just do nothing
}

// loop dequeue and process jobs
func (j *Jobs) loop(process func(Job) error, ctx *snow.ConsensusContext, halter common.Haltable, restarted bool) (int, error) {
	ctx.Executing(true)
	defer ctx.Executing(false)

	numExecuted := 0

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
			ctx.Log.Info("Interrupted execution after executing %d operations", numExecuted)
			return numExecuted, nil
		}

		// retrieve next runnable job
		job, err := j.state.RemoveRunnableJob()
		if err == database.ErrNotFound {
			break
		} else if err != nil {
			return numExecuted, fmt.Errorf("failed to removing runnable job with %w", err)
		}

		// process runnable job
		if err := process(job); err != nil {
			return numExecuted, fmt.Errorf("failed to execute job %s due to %w", job.ID(), err)
		}

		// update dependencies and runnable jobs
		dependentIDs, err := j.state.RemoveDependencies(job.ID())
		if err != nil {
			return numExecuted, fmt.Errorf("failed to remove blocking jobs for %s due to %w", job.ID(), err)
		}
		for _, dependentID := range dependentIDs {
			job, err := j.state.GetJob(dependentID)
			if err != nil {
				return numExecuted, fmt.Errorf("failed to get job %s from blocking jobs due to %w", dependentID, err)
			}
			hasMissingDeps, err := job.HasMissingDependencies()
			if err != nil {
				return numExecuted, fmt.Errorf("failed to get missing dependencies for %s due to %w", dependentID, err)
			} else if hasMissingDeps {
				continue
			}
			if err := j.state.AddRunnableJob(dependentID); err != nil {
				return numExecuted, fmt.Errorf("failed to add %s as a runnable job due to %w", dependentID, err)
			}
		}

		// commit and move to next job
		if err := j.Commit(); err != nil {
			return numExecuted, err
		}
		numExecuted++
		if numExecuted%StatusUpdateFrequency == 0 { // Periodically print progress
			if !restarted {
				ctx.Log.Info("executed %d operations", numExecuted)
			} else {
				ctx.Log.Debug("executed %d operations", numExecuted)
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
