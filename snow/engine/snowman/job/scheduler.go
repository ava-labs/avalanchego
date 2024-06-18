// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package job provides a Scheduler to manage and execute Jobs with
// dependencies.
package job

import (
	"context"

	"github.com/ava-labs/avalanchego/utils/set"
)

// Job is a unit of work that can be executed or cancelled.
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

// Scheduler implements a dependency graph for jobs. Jobs can be registered with
// dependencies, and once all dependencies are fulfilled, the job will be
// executed. If any of the dependencies are abandoned, the job will be
// cancelled.
type Scheduler[T comparable] struct {
	// dependents maps a dependency to the jobs that depend on it.
	dependents map[T][]*job[T]
}

func NewScheduler[T comparable]() *Scheduler[T] {
	return &Scheduler[T]{
		dependents: make(map[T][]*job[T]),
	}
}

// Register a job that should be executed once all of its dependencies are
// fulfilled. In order to prevent a memory leak, all dependencies must
// eventually either be fulfilled or abandoned.
//
// While registering a job with duplicate dependencies is discouraged, it is
// allowed and treated similarly to registering the job with the dependencies
// de-duplicated.
func (s *Scheduler[T]) Register(ctx context.Context, userJob Job, dependencies ...T) error {
	if len(dependencies) == 0 {
		return userJob.Execute(ctx)
	}

	j := &job[T]{
		dependencies: set.Of(dependencies...),
		job:          userJob,
	}
	for _, d := range dependencies {
		s.dependents[d] = append(s.dependents[d], j)
	}
	return nil
}

// NumDependencies returns the number of dependencies that jobs are currently
// blocking on.
func (s *Scheduler[_]) NumDependencies() int {
	return len(s.dependents)
}

// Fulfill a dependency. If all dependencies for a job are fulfilled, the job
// will be executed.
//
// It is safe to call the scheduler during the execution of a job.
func (s *Scheduler[T]) Fulfill(ctx context.Context, dependency T) error {
	return s.resolveDependency(ctx, dependency, false)
}

// Abandon a dependency. If any dependencies for a job are abandoned, the job
// will be cancelled. The job will only be cancelled once all dependencies are
// resolved.
//
// It is safe to call the scheduler during the cancelling of a job.
func (s *Scheduler[T]) Abandon(ctx context.Context, dependency T) error {
	return s.resolveDependency(ctx, dependency, true)
}

func (s *Scheduler[T]) resolveDependency(
	ctx context.Context,
	dependency T,
	shouldCancel bool,
) error {
	jobs := s.dependents[dependency]
	delete(s.dependents, dependency)

	for _, job := range jobs {
		job.dependencies.Remove(dependency)
		job.shouldCancel = shouldCancel || job.shouldCancel

		userJob := job.job
		if userJob == nil || job.dependencies.Len() != 0 {
			continue
		}

		// Mark the job as handled so that any reentrant calls do not interact
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
