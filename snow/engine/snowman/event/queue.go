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
	dependencies set.Set[T]
	job          Job
}

type Queue[T comparable] struct {
	jobs map[T][]*job[T]
}

func NewQueue[T comparable]() *Queue[T] {
	return &Queue[T]{
		jobs: make(map[T][]*job[T]),
	}
}

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

func (q *Queue[_]) Len() int {
	return len(q.jobs)
}

func (q *Queue[T]) Fulfill(ctx context.Context, dependency T) error {
	jobs := q.jobs[dependency]
	delete(q.jobs, dependency)

	for _, job := range jobs {
		job.dependencies.Remove(dependency)

		userJob := job.job
		if userJob == nil || job.dependencies.Len() != 0 {
			continue
		}

		// If the job was registered with duplicate dependencies, it may be
		// possible for the job to be executed multiple times. To prevent this,
		// we clear the job.
		job.job = nil

		if err := userJob.Execute(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (q *Queue[T]) Abandon(ctx context.Context, dependency T) error {
	jobs := q.jobs[dependency]
	delete(q.jobs, dependency)

	for _, job := range jobs {
		job.dependencies.Remove(dependency)

		userJob := job.job
		if userJob == nil {
			continue
		}

		// Mark the job as cancelled so that any reentrant calls do not interact
		// with this job again.
		job.job = nil

		if err := userJob.Cancel(ctx); err != nil {
			return err
		}
	}
	return nil
}
