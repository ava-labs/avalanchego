// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package job

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	depToResolve = iota
	depToNeglect
)

var errDuplicateExecution = errors.New("job already executed")

type testJob struct {
	calledExecute bool
	fulfilled     []int
	abandoned     []int
}

func (j *testJob) Execute(_ context.Context, fulfilled []int, abandoned []int) error {
	if j.calledExecute {
		return errDuplicateExecution
	}
	j.calledExecute = true
	j.fulfilled = fulfilled
	j.abandoned = abandoned
	return nil
}

func (j *testJob) reset() {
	j.calledExecute = false
	j.fulfilled = nil
	j.abandoned = nil
}

func newSchedulerWithJob[T comparable](
	t *testing.T,
	job Job[T],
	dependencies []T,
	fulfilled []T,
	abandoned []T,
) *Scheduler[T] {
	s := NewScheduler[T]()
	require.NoError(t, s.Schedule(context.Background(), job, dependencies...))
	for _, d := range fulfilled {
		require.NoError(t, s.Fulfill(context.Background(), d))
	}
	for _, d := range abandoned {
		require.NoError(t, s.Abandon(context.Background(), d))
	}
	return s
}

func TestScheduler_Schedule(t *testing.T) {
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
							numUnresolved: 1,
							fulfilled:     nil,
							abandoned:     nil,
							job:           userJob,
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
							numUnresolved: 2,
							fulfilled:     nil,
							abandoned:     nil,
							job:           userJob,
						},
					},
					depToNeglect: {
						{
							numUnresolved: 2,
							fulfilled:     nil,
							abandoned:     nil,
							job:           userJob,
						},
					},
				},
			},
		},
		{
			name:                "additional dependency",
			scheduler:           newSchedulerWithJob(t, userJob, []int{depToResolve}, nil, nil),
			dependencies:        []int{depToResolve},
			wantExecuted:        false,
			wantNumDependencies: 1,
			wantScheduler: &Scheduler[int]{
				dependents: map[int][]*job[int]{
					depToResolve: {
						{
							numUnresolved: 1,
							fulfilled:     nil,
							abandoned:     nil,
							job:           userJob,
						},
						{
							numUnresolved: 1,
							fulfilled:     nil,
							abandoned:     nil,
							job:           userJob,
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

			require.NoError(test.scheduler.Schedule(context.Background(), userJob, test.dependencies...))
			require.Equal(test.wantNumDependencies, test.scheduler.NumDependencies())
			require.Equal(test.wantExecuted, userJob.calledExecute)
			require.Empty(userJob.fulfilled)
			require.Empty(userJob.abandoned)
			require.Equal(test.wantScheduler, test.scheduler)
		})
	}
}

func TestScheduler_Fulfill(t *testing.T) {
	userJob := &testJob{}
	tests := []struct {
		name          string
		scheduler     *Scheduler[int]
		wantExecuted  bool
		wantFulfilled []int
		wantAbandoned []int
		wantScheduler *Scheduler[int]
	}{
		{
			name:          "no jobs",
			scheduler:     NewScheduler[int](),
			wantExecuted:  false,
			wantFulfilled: nil,
			wantAbandoned: nil,
			wantScheduler: NewScheduler[int](),
		},
		{
			name:          "single dependency",
			scheduler:     newSchedulerWithJob(t, userJob, []int{depToResolve}, nil, nil),
			wantExecuted:  true,
			wantFulfilled: []int{depToResolve},
			wantAbandoned: nil,
			wantScheduler: NewScheduler[int](),
		},
		{
			name:          "non-existent dependency",
			scheduler:     newSchedulerWithJob(t, userJob, []int{depToNeglect}, nil, nil),
			wantExecuted:  false,
			wantFulfilled: nil,
			wantAbandoned: nil,
			wantScheduler: newSchedulerWithJob(t, userJob, []int{depToNeglect}, nil, nil),
		},
		{
			name:          "incomplete dependencies",
			scheduler:     newSchedulerWithJob(t, userJob, []int{depToResolve, depToNeglect}, nil, nil),
			wantExecuted:  false,
			wantFulfilled: nil,
			wantAbandoned: nil,
			wantScheduler: &Scheduler[int]{
				dependents: map[int][]*job[int]{
					depToNeglect: {
						{
							numUnresolved: 1,
							fulfilled:     []int{depToResolve},
							abandoned:     nil,
							job:           userJob,
						},
					},
				},
			},
		},
		{
			name:          "duplicate dependency",
			scheduler:     newSchedulerWithJob(t, userJob, []int{depToResolve, depToResolve}, nil, nil),
			wantExecuted:  true,
			wantFulfilled: []int{depToResolve, depToResolve},
			wantAbandoned: nil,
			wantScheduler: NewScheduler[int](),
		},
		{
			name:          "previously abandoned",
			scheduler:     newSchedulerWithJob(t, userJob, []int{depToResolve, depToNeglect}, nil, []int{depToNeglect}),
			wantExecuted:  true,
			wantFulfilled: []int{depToResolve},
			wantAbandoned: []int{depToNeglect},
			wantScheduler: NewScheduler[int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Reset the variable between tests
			userJob.reset()

			require.NoError(test.scheduler.Fulfill(context.Background(), depToResolve))
			require.Equal(test.wantExecuted, userJob.calledExecute)
			require.Equal(test.wantFulfilled, userJob.fulfilled)
			require.Equal(test.wantAbandoned, userJob.abandoned)
			require.Equal(test.wantScheduler, test.scheduler)
		})
	}
}

func TestScheduler_Abandon(t *testing.T) {
	userJob := &testJob{}
	tests := []struct {
		name          string
		scheduler     *Scheduler[int]
		wantExecuted  bool
		wantFulfilled []int
		wantAbandoned []int
		wantScheduler *Scheduler[int]
	}{
		{
			name:          "no jobs",
			scheduler:     NewScheduler[int](),
			wantExecuted:  false,
			wantFulfilled: nil,
			wantAbandoned: nil,
			wantScheduler: NewScheduler[int](),
		},
		{
			name:          "single dependency",
			scheduler:     newSchedulerWithJob(t, userJob, []int{depToResolve}, nil, nil),
			wantExecuted:  true,
			wantFulfilled: nil,
			wantAbandoned: []int{depToResolve},
			wantScheduler: NewScheduler[int](),
		},
		{
			name:          "non-existent dependency",
			scheduler:     newSchedulerWithJob(t, userJob, []int{depToNeglect}, nil, nil),
			wantExecuted:  false,
			wantFulfilled: nil,
			wantAbandoned: nil,
			wantScheduler: newSchedulerWithJob(t, userJob, []int{depToNeglect}, nil, nil),
		},
		{
			name:          "incomplete dependencies",
			scheduler:     newSchedulerWithJob(t, userJob, []int{depToResolve, depToNeglect}, nil, nil),
			wantExecuted:  false,
			wantFulfilled: nil,
			wantAbandoned: nil,
			wantScheduler: &Scheduler[int]{
				dependents: map[int][]*job[int]{
					depToNeglect: {
						{
							numUnresolved: 1,
							fulfilled:     nil,
							abandoned:     []int{depToResolve},
							job:           userJob,
						},
					},
				},
			},
		},
		{
			name:          "duplicate dependency",
			scheduler:     newSchedulerWithJob(t, userJob, []int{depToResolve, depToResolve}, nil, nil),
			wantExecuted:  true,
			wantFulfilled: nil,
			wantAbandoned: []int{depToResolve, depToResolve},
			wantScheduler: NewScheduler[int](),
		},
		{
			name:          "previously fulfilled",
			scheduler:     newSchedulerWithJob(t, userJob, []int{depToResolve, depToNeglect}, []int{depToNeglect}, nil),
			wantExecuted:  true,
			wantFulfilled: []int{depToNeglect},
			wantAbandoned: []int{depToResolve},
			wantScheduler: NewScheduler[int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Reset the variable between tests
			userJob.reset()

			require.NoError(test.scheduler.Abandon(context.Background(), depToResolve))
			require.Equal(test.wantExecuted, userJob.calledExecute)
			require.Equal(test.wantFulfilled, userJob.fulfilled)
			require.Equal(test.wantAbandoned, userJob.abandoned)
			require.Equal(test.wantScheduler, test.scheduler)
		})
	}
}
