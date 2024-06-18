// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package event

import (
	"context"

	"github.com/ava-labs/avalanchego/utils/set"
)

type Job interface {
	Execute(context.Context) error
	Cancel(context.Context) error
}

type job[T comparable] struct {
	// If empty, the job is ready to be executed.
	dependencies set.Set[T]
	// If true, the job should be cancelled.
	shouldCancel bool
	// If nil, the job has already been executed or cancelled.
	job Job
}

type Queue[T comparable] struct {
	// jobs maps a dependency to the jobs that depend on it.
	jobs map[T][]*job[T]
}

func NewQueue[T comparable]() *Queue[T] {
	return &Queue[T]{
		jobs: make(map[T][]*job[T]),
	}
}

// Register a job that should be executed once all of its dependencies are
// fulfilled. In order to prevent a memory leak, all dependencies must
// eventually either be fulfilled or abandoned.
//
// While registering a job with duplicate dependencies is discouraged, it is
// allowed and treated similarly to registering the job with the dependencies
// de-duplicated.
func (q *Queue[T]) Register(ctx context.Context, userJob Job, dependencies ...T) error {
	if len(dependencies) == 0 {
		return userJob.Execute(ctx)
	}

	j := &job[T]{
		dependencies: set.Of(dependencies...),
		job:          userJob,
	}
	for _, dependency := range dependencies {
		q.jobs[dependency] = append(q.jobs[dependency], j)
	}
	return nil
}

// NumDependencies returns the number of dependencies that jobs are currently
// blocking on.
func (q *Queue[_]) NumDependencies() int {
	return len(q.jobs)
}

// Fulfill a dependency. If all dependencies for a job are fulfilled, the job
// will be executed.
//
// It is safe to call the queue during the execution of a job.
func (q *Queue[T]) Fulfill(ctx context.Context, dependency T) error {
	return q.resolveDependency(ctx, dependency, false)
}

// Abandon a dependency. If any dependencies for a job are abandoned, the job
// will be cancelled. The job will only be cancelled once all dependencies are
// resolved.
//
// It is safe to call the queue during the cancelling of a job.
func (q *Queue[T]) Abandon(ctx context.Context, dependency T) error {
	return q.resolveDependency(ctx, dependency, true)
}

func (q *Queue[T]) resolveDependency(
	ctx context.Context,
	dependency T,
	shouldCancel bool,
) error {
	jobs := q.jobs[dependency]
	delete(q.jobs, dependency)

	for _, job := range jobs {
		// Removing the dependency keeps the queue in a consistent state.
		// However, it isn't strictly needed.
		job.dependencies.Remove(dependency)
		job.shouldCancel = shouldCancel || job.shouldCancel

		userJob := job.job
		if userJob == nil || job.dependencies.Len() != 0 {
			continue
		}

		// Mark the job as cancelled so that any reentrant calls do not interact
		// with this job again.
		job.job = nil

		var err error
		if job.shouldCancel {
			err = userJob.Cancel(ctx)
		} else {
			err = userJob.Execute(ctx)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
