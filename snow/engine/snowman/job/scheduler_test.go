// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	require.NoError(t, s.Schedule(t.Context(), job, dependencies...))
	for _, d := range fulfilled {
		require.NoError(t, s.Fulfill(t.Context(), d))
	}
	for _, d := range abandoned {
		require.NoError(t, s.Abandon(t.Context(), d))
	}
	return s
}

// chainJob propagates its parent's resolution to the next job in the chain.
type chainJob struct {
	scheduler  *Scheduler[int]
	dependency int
	depth      *int
	maxDepth   *int
}

func (j *chainJob) Execute(ctx context.Context, fulfilled []int, _ []int) error {
	*j.depth++
	*j.maxDepth = max(*j.maxDepth, *j.depth)
	defer func() {
		*j.depth--
	}()

	if len(fulfilled) > 0 {
		return j.scheduler.Fulfill(ctx, j.dependency)
	}
	return j.scheduler.Abandon(ctx, j.dependency)
}

func TestScheduler_Schedule(t *testing.T) {
	userJob := &testJob{}
	tests := []struct {
		name                    string
		scheduler               *Scheduler[int]
		dependencies            []int
		expectedExecuted        bool
		expectedNumDependencies int
		expectedScheduler       *Scheduler[int]
	}{
		{
			name:                    "no dependencies",
			scheduler:               NewScheduler[int](),
			dependencies:            nil,
			expectedExecuted:        true,
			expectedNumDependencies: 0,
			expectedScheduler:       NewScheduler[int](),
		},
		{
			name:                    "one dependency",
			scheduler:               NewScheduler[int](),
			dependencies:            []int{depToResolve},
			expectedExecuted:        false,
			expectedNumDependencies: 1,
			expectedScheduler: &Scheduler[int]{
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
			name:                    "two dependencies",
			scheduler:               NewScheduler[int](),
			dependencies:            []int{depToResolve, depToNeglect},
			expectedExecuted:        false,
			expectedNumDependencies: 2,
			expectedScheduler: &Scheduler[int]{
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
			name:                    "additional dependency",
			scheduler:               newSchedulerWithJob(t, userJob, []int{depToResolve}, nil, nil),
			dependencies:            []int{depToResolve},
			expectedExecuted:        false,
			expectedNumDependencies: 1,
			expectedScheduler: &Scheduler[int]{
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

			require.NoError(test.scheduler.Schedule(t.Context(), userJob, test.dependencies...))
			require.Equal(test.expectedNumDependencies, test.scheduler.NumDependencies())
			require.Equal(test.expectedExecuted, userJob.calledExecute)
			require.Empty(userJob.fulfilled)
			require.Empty(userJob.abandoned)
			require.Equal(test.expectedScheduler, test.scheduler)
		})
	}
}

func TestScheduler_Fulfill(t *testing.T) {
	userJob := &testJob{}
	tests := []struct {
		name              string
		scheduler         *Scheduler[int]
		expectedExecuted  bool
		expectedFulfilled []int
		expectedAbandoned []int
		expectedScheduler *Scheduler[int]
	}{
		{
			name:              "no jobs",
			scheduler:         NewScheduler[int](),
			expectedExecuted:  false,
			expectedFulfilled: nil,
			expectedAbandoned: nil,
			expectedScheduler: NewScheduler[int](),
		},
		{
			name:              "single dependency",
			scheduler:         newSchedulerWithJob(t, userJob, []int{depToResolve}, nil, nil),
			expectedExecuted:  true,
			expectedFulfilled: []int{depToResolve},
			expectedAbandoned: nil,
			expectedScheduler: NewScheduler[int](),
		},
		{
			name:              "non-existent dependency",
			scheduler:         newSchedulerWithJob(t, userJob, []int{depToNeglect}, nil, nil),
			expectedExecuted:  false,
			expectedFulfilled: nil,
			expectedAbandoned: nil,
			expectedScheduler: newSchedulerWithJob(t, userJob, []int{depToNeglect}, nil, nil),
		},
		{
			name:              "incomplete dependencies",
			scheduler:         newSchedulerWithJob(t, userJob, []int{depToResolve, depToNeglect}, nil, nil),
			expectedExecuted:  false,
			expectedFulfilled: nil,
			expectedAbandoned: nil,
			expectedScheduler: &Scheduler[int]{
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
			name:              "duplicate dependency",
			scheduler:         newSchedulerWithJob(t, userJob, []int{depToResolve, depToResolve}, nil, nil),
			expectedExecuted:  true,
			expectedFulfilled: []int{depToResolve, depToResolve},
			expectedAbandoned: nil,
			expectedScheduler: NewScheduler[int](),
		},
		{
			name:              "previously abandoned",
			scheduler:         newSchedulerWithJob(t, userJob, []int{depToResolve, depToNeglect}, nil, []int{depToNeglect}),
			expectedExecuted:  true,
			expectedFulfilled: []int{depToResolve},
			expectedAbandoned: []int{depToNeglect},
			expectedScheduler: NewScheduler[int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Reset the variable between tests
			userJob.reset()

			require.NoError(test.scheduler.Fulfill(t.Context(), depToResolve))
			require.Equal(test.expectedExecuted, userJob.calledExecute)
			require.Equal(test.expectedFulfilled, userJob.fulfilled)
			require.Equal(test.expectedAbandoned, userJob.abandoned)
			require.Equal(test.expectedScheduler, test.scheduler)
		})
	}
}

func TestScheduler_Abandon(t *testing.T) {
	userJob := &testJob{}
	tests := []struct {
		name              string
		scheduler         *Scheduler[int]
		expectedExecuted  bool
		expectedFulfilled []int
		expectedAbandoned []int
		expectedScheduler *Scheduler[int]
	}{
		{
			name:              "no jobs",
			scheduler:         NewScheduler[int](),
			expectedExecuted:  false,
			expectedFulfilled: nil,
			expectedAbandoned: nil,
			expectedScheduler: NewScheduler[int](),
		},
		{
			name:              "single dependency",
			scheduler:         newSchedulerWithJob(t, userJob, []int{depToResolve}, nil, nil),
			expectedExecuted:  true,
			expectedFulfilled: nil,
			expectedAbandoned: []int{depToResolve},
			expectedScheduler: NewScheduler[int](),
		},
		{
			name:              "non-existent dependency",
			scheduler:         newSchedulerWithJob(t, userJob, []int{depToNeglect}, nil, nil),
			expectedExecuted:  false,
			expectedFulfilled: nil,
			expectedAbandoned: nil,
			expectedScheduler: newSchedulerWithJob(t, userJob, []int{depToNeglect}, nil, nil),
		},
		{
			name:              "incomplete dependencies",
			scheduler:         newSchedulerWithJob(t, userJob, []int{depToResolve, depToNeglect}, nil, nil),
			expectedExecuted:  false,
			expectedFulfilled: nil,
			expectedAbandoned: nil,
			expectedScheduler: &Scheduler[int]{
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
			name:              "duplicate dependency",
			scheduler:         newSchedulerWithJob(t, userJob, []int{depToResolve, depToResolve}, nil, nil),
			expectedExecuted:  true,
			expectedFulfilled: nil,
			expectedAbandoned: []int{depToResolve, depToResolve},
			expectedScheduler: NewScheduler[int](),
		},
		{
			name:              "previously fulfilled",
			scheduler:         newSchedulerWithJob(t, userJob, []int{depToResolve, depToNeglect}, []int{depToNeglect}, nil),
			expectedExecuted:  true,
			expectedFulfilled: []int{depToNeglect},
			expectedAbandoned: []int{depToResolve},
			expectedScheduler: NewScheduler[int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Reset the variable between tests
			userJob.reset()

			require.NoError(test.scheduler.Abandon(t.Context(), depToResolve))
			require.Equal(test.expectedExecuted, userJob.calledExecute)
			require.Equal(test.expectedFulfilled, userJob.fulfilled)
			require.Equal(test.expectedAbandoned, userJob.abandoned)
			require.Equal(test.expectedScheduler, test.scheduler)
		})
	}
}

// Resolving a long chain of dependent jobs must not nest Execute calls, since
// a drain that recurses one frame per job overflows the stack.
func TestScheduler_LongChainDoesNotRecurse(t *testing.T) {
	const chainLen = 50_000

	tests := []struct {
		name      string
		fulfilled bool
	}{
		{
			name:      "fulfill",
			fulfilled: true,
		},
		{
			name:      "abandon",
			fulfilled: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			s := NewScheduler[int]()
			depth, maxDepth := 0, 0
			for k := 1; k <= chainLen; k++ {
				job := &chainJob{
					scheduler:  s,
					dependency: k,
					depth:      &depth,
					maxDepth:   &maxDepth,
				}
				require.NoError(s.Schedule(t.Context(), job, k-1))
			}

			if test.fulfilled {
				require.NoError(s.Fulfill(t.Context(), 0))
			} else {
				require.NoError(s.Abandon(t.Context(), 0))
			}
			require.Equal(1, maxDepth)
		})
	}
}
