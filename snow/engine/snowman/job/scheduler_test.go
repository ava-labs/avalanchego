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

var errDuplicateInvocation = errors.New("job already handled")

type testJob struct {
	calledExecute bool
	calledCancel  bool
}

func (j *testJob) Execute(context.Context) error {
	if j.calledExecute {
		return errDuplicateInvocation
	}
	j.calledExecute = true
	return nil
}

func (j *testJob) Cancel(context.Context) error {
	if j.calledCancel {
		return errDuplicateInvocation
	}
	j.calledCancel = true
	return nil
}

func (j *testJob) reset() {
	j.calledExecute = false
	j.calledCancel = false
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
	userJob := &testJob{}
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
			userJob.reset()

			require.NoError(test.scheduler.Register(context.Background(), userJob, test.dependencies...))
			require.Equal(test.wantNumDependencies, test.scheduler.NumDependencies())
			require.Equal(test.wantExecuted, userJob.calledExecute)
			require.False(userJob.calledCancel)
			require.Equal(test.wantScheduler, test.scheduler)
		})
	}
}

func TestScheduler_Fulfill(t *testing.T) {
	userJob := &testJob{}
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
			userJob.reset()

			require.NoError(test.scheduler.Fulfill(context.Background(), depToResolve))
			require.Equal(test.wantExecute, userJob.calledExecute)
			require.False(userJob.calledCancel)
			require.Equal(test.wantScheduler, test.scheduler)
		})
	}
}

func TestScheduler_Abandon(t *testing.T) {
	userJob := &testJob{}
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
			userJob.reset()

			require.NoError(test.scheduler.Abandon(context.Background(), depToResolve))
			require.False(userJob.calledExecute)
			require.Equal(test.wantCancelled, userJob.calledCancel)
			require.Equal(test.wantScheduler, test.scheduler)
		})
	}
}
