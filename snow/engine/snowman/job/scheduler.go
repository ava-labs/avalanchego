// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package job provides a Scheduler to manage and execute Jobs with
// dependencies.
package job

import "context"

// Job is a unit of work that can be executed based on the result of resolving
// requested dependencies.
type Job[T any] interface {
	Execute(ctx context.Context, fulfilled []T, abandoned []T) error
}

type job[T comparable] struct {
	// Once all dependencies are resolved, the job will be executed.
	numUnresolved int
	fulfilled     []T
	abandoned     []T
	job           Job[T]
}

// Scheduler implements a dependency graph for jobs. Jobs can be registered with
// dependencies, and once all dependencies are resolved, the job will be
// executed.
type Scheduler[T comparable] struct {
	// dependents maps a dependency to the jobs that depend on it.
	dependents map[T][]*job[T]
}

func NewScheduler[T comparable]() *Scheduler[T] {
	return &Scheduler[T]{
		dependents: make(map[T][]*job[T]),
	}
}

// Schedule a job to be executed once all of its dependencies are resolved. If a
// job is scheduled with no dependencies, it's executed immediately.
//
// In order to prevent a memory leak, all dependencies must eventually either be
// fulfilled or abandoned.
//
// While registering a job with duplicate dependencies is discouraged, it is
// allowed.
func (s *Scheduler[T]) Schedule(ctx context.Context, userJob Job[T], dependencies ...T) error {
	numUnresolved := len(dependencies)
	if numUnresolved == 0 {
		return userJob.Execute(ctx, nil, nil)
	}

	j := &job[T]{
		numUnresolved: numUnresolved,
		job:           userJob,
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

// Fulfill a dependency. If all dependencies for a job are resolved, the job
// will be executed.
//
// It is safe to call the scheduler during the execution of a job.
func (s *Scheduler[T]) Fulfill(ctx context.Context, dependency T) error {
	return s.resolveDependency(ctx, dependency, true)
}

// Abandon a dependency. If all dependencies for a job are resolved, the job
// will be executed.
//
// It is safe to call the scheduler during the execution of a job.
func (s *Scheduler[T]) Abandon(ctx context.Context, dependency T) error {
	return s.resolveDependency(ctx, dependency, false)
}

func (s *Scheduler[T]) resolveDependency(
	ctx context.Context,
	dependency T,
	fulfilled bool,
) error {
	jobs := s.dependents[dependency]
	delete(s.dependents, dependency)

	for _, job := range jobs {
		job.numUnresolved--
		if fulfilled {
			job.fulfilled = append(job.fulfilled, dependency)
		} else {
			job.abandoned = append(job.abandoned, dependency)
		}

		if job.numUnresolved > 0 {
			continue
		}

		if err := job.job.Execute(ctx, job.fulfilled, job.abandoned); err != nil {
			return err
		}
	}
	return nil
}
