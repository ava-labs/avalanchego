// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package job

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	depToResolve = iota
	depToNeglect
)

var (
	errDuplicateInvocation  = errors.New("job already handled")
	errUnexpectedInvocation = errors.New("job handled unexpectedly")
)

type testJob struct {
	execute func(context.Context) error
	cancel  func(context.Context) error
}

func (j *testJob) Execute(ctx context.Context) error {
	return j.execute(ctx)
}

func (j *testJob) Cancel(ctx context.Context) error {
	return j.cancel(ctx)
}

func newSchedulerWithJob[T comparable](
	t *testing.T,
	job Job,
	shouldCancel bool,
	dependencies ...T,
) *Scheduler[T] {
	q := NewScheduler[T]()
	require.NoError(t, q.Register(context.Background(), job, dependencies...))
	if shouldCancel {
		for _, jobs := range q.dependents {
			for _, j := range jobs {
				j.shouldCancel = true
			}
		}
	}
	return q
}

func TestScheduler_Register(t *testing.T) {
	var calledExecute bool
	userJob := &testJob{
		execute: func(context.Context) error {
			if calledExecute {
				return errDuplicateInvocation
			}
			calledExecute = true
			return nil
		},
		cancel: func(context.Context) error {
			return errUnexpectedInvocation
		},
	}

	tests := []struct {
		name                string
		scheduler           *Scheduler[int]
		dependencies        []int
		wantExecuted        bool
		wantNumDependencies int
		wantScheduler       *Scheduler[int]
	}{
		{
			name:                "no dependencies",
			scheduler:           NewScheduler[int](),
			dependencies:        nil,
			wantExecuted:        true,
			wantNumDependencies: 0,
			wantScheduler:       NewScheduler[int](),
		},
		{
			name:                "one dependency",
			scheduler:           NewScheduler[int](),
			dependencies:        []int{depToResolve},
			wantExecuted:        false,
			wantNumDependencies: 1,
			wantScheduler: &Scheduler[int]{
				dependents: map[int][]*job[int]{
					depToResolve: {
						{
							dependencies: set.Of(depToResolve),
							job:          userJob,
						},
					},
				},
			},
		},
		{
			name:                "two dependencies",
			scheduler:           NewScheduler[int](),
			dependencies:        []int{depToResolve, depToNeglect},
			wantExecuted:        false,
			wantNumDependencies: 2,
			wantScheduler: &Scheduler[int]{
				dependents: map[int][]*job[int]{
					depToResolve: {
						{
							dependencies: set.Of(depToResolve, depToNeglect),
							job:          userJob,
						},
					},
					depToNeglect: {
						{
							dependencies: set.Of(depToResolve, depToNeglect),
							job:          userJob,
						},
					},
				},
			},
		},
		{
			name:                "additional dependency",
			scheduler:           newSchedulerWithJob(t, userJob, false, depToResolve),
			dependencies:        []int{depToResolve},
			wantExecuted:        false,
			wantNumDependencies: 1,
			wantScheduler: &Scheduler[int]{
				dependents: map[int][]*job[int]{
					depToResolve: {
						{
							dependencies: set.Of(depToResolve),
							job:          userJob,
						},
						{
							dependencies: set.Of(depToResolve),
							job:          userJob,
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Reset the variable between tests
			calledExecute = false

			require.NoError(test.scheduler.Register(context.Background(), userJob, test.dependencies...))
			require.Equal(test.wantNumDependencies, test.scheduler.NumDependencies())
			require.Equal(test.wantExecuted, calledExecute)
			require.Equal(test.wantScheduler, test.scheduler)
		})
	}
}

func TestScheduler_Fulfill(t *testing.T) {
	var calledExecute bool
	userJob := &testJob{
		execute: func(context.Context) error {
			if calledExecute {
				return errDuplicateInvocation
			}
			calledExecute = true
			return nil
		},
		cancel: func(context.Context) error {
			return errUnexpectedInvocation
		},
	}

	tests := []struct {
		name          string
		scheduler     *Scheduler[int]
		wantExecute   bool
		wantScheduler *Scheduler[int]
	}{
		{
			name:          "no jobs",
			scheduler:     NewScheduler[int](),
			wantExecute:   false,
			wantScheduler: NewScheduler[int](),
		},
		{
			name:          "single dependency",
			scheduler:     newSchedulerWithJob(t, userJob, false, depToResolve),
			wantExecute:   true,
			wantScheduler: NewScheduler[int](),
		},
		{
			name:          "non-existent dependency",
			scheduler:     newSchedulerWithJob(t, userJob, false, depToNeglect),
			wantExecute:   false,
			wantScheduler: newSchedulerWithJob(t, userJob, false, depToNeglect),
		},
		{
			name:          "incomplete dependencies",
			scheduler:     newSchedulerWithJob(t, userJob, false, depToResolve, depToNeglect),
			wantExecute:   false,
			wantScheduler: newSchedulerWithJob(t, userJob, false, depToNeglect),
		},
		{
			name:          "duplicate dependency",
			scheduler:     newSchedulerWithJob(t, userJob, false, depToResolve, depToResolve),
			wantExecute:   true,
			wantScheduler: NewScheduler[int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Reset the variable between tests
			calledExecute = false

			require.NoError(test.scheduler.Fulfill(context.Background(), depToResolve))
			require.Equal(test.wantExecute, calledExecute)
			require.Equal(test.wantScheduler, test.scheduler)
		})
	}
}

func TestScheduler_Abandon(t *testing.T) {
	var calledCancel bool
	userJob := &testJob{
		execute: func(context.Context) error {
			return errUnexpectedInvocation
		},
		cancel: func(context.Context) error {
			if calledCancel {
				return errDuplicateInvocation
			}
			calledCancel = true
			return nil
		},
	}

	tests := []struct {
		name          string
		scheduler     *Scheduler[int]
		wantCancelled bool
		wantScheduler *Scheduler[int]
	}{
		{
			name:          "no jobs",
			scheduler:     NewScheduler[int](),
			wantCancelled: false,
			wantScheduler: NewScheduler[int](),
		},
		{
			name:          "single dependency",
			scheduler:     newSchedulerWithJob(t, userJob, false, depToResolve),
			wantCancelled: true,
			wantScheduler: NewScheduler[int](),
		},
		{
			name:          "non-existent dependency",
			scheduler:     newSchedulerWithJob(t, userJob, false, depToNeglect),
			wantCancelled: false,
			wantScheduler: newSchedulerWithJob(t, userJob, false, depToNeglect),
		},
		{
			name:          "incomplete dependencies",
			scheduler:     newSchedulerWithJob(t, userJob, false, depToResolve, depToNeglect),
			wantCancelled: false,
			wantScheduler: newSchedulerWithJob(t, userJob, true, depToNeglect),
		},
		{
			name:          "duplicate dependency",
			scheduler:     newSchedulerWithJob(t, userJob, false, depToResolve, depToResolve),
			wantCancelled: true,
			wantScheduler: NewScheduler[int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Reset the variable between tests
			calledCancel = false

			require.NoError(test.scheduler.Abandon(context.Background(), depToResolve))
			require.Equal(test.wantCancelled, calledCancel)
			require.Equal(test.wantScheduler, test.scheduler)
		})
	}
}
