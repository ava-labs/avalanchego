// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package event

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

func newQueueWithJob[T comparable](t *testing.T, job Job, shouldCancel bool, dependencies ...T) *Queue[T] {
	q := NewQueue[T]()
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

func TestQueue_Register(t *testing.T) {
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
		name         string
		queue        *Queue[int]
		dependencies []int
		wantExecuted bool
		wantLen      int
		wantQueue    *Queue[int]
	}{
		{
			name:         "no dependencies",
			queue:        NewQueue[int](),
			dependencies: nil,
			wantExecuted: true,
			wantLen:      0,
			wantQueue:    NewQueue[int](),
		},
		{
			name:         "one dependency",
			queue:        NewQueue[int](),
			dependencies: []int{depToResolve},
			wantExecuted: false,
			wantLen:      1,
			wantQueue: &Queue[int]{
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
			name:         "two dependencies",
			queue:        NewQueue[int](),
			dependencies: []int{depToResolve, depToNeglect},
			wantExecuted: false,
			wantLen:      2,
			wantQueue: &Queue[int]{
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
			name:         "additional dependency",
			queue:        newQueueWithJob(t, userJob, false, depToResolve),
			dependencies: []int{depToResolve},
			wantExecuted: false,
			wantLen:      1,
			wantQueue: &Queue[int]{
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

			require.NoError(test.queue.Register(context.Background(), userJob, test.dependencies...))
			require.Equal(test.wantLen, test.queue.NumDependencies())
			require.Equal(test.wantExecuted, calledExecute)
			require.Equal(test.wantQueue, test.queue)
		})
	}
}

func TestQueue_Fulfill(t *testing.T) {
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
		name        string
		queue       *Queue[int]
		wantExecute bool
		wantQueue   *Queue[int]
	}{
		{
			name:        "no jobs",
			queue:       NewQueue[int](),
			wantExecute: false,
			wantQueue:   NewQueue[int](),
		},
		{
			name:        "single dependency",
			queue:       newQueueWithJob(t, userJob, false, depToResolve),
			wantExecute: true,
			wantQueue:   NewQueue[int](),
		},
		{
			name:        "non-existent dependency",
			queue:       newQueueWithJob(t, userJob, false, depToNeglect),
			wantExecute: false,
			wantQueue:   newQueueWithJob(t, userJob, false, depToNeglect),
		},
		{
			name:        "incomplete dependencies",
			queue:       newQueueWithJob(t, userJob, false, depToResolve, depToNeglect),
			wantExecute: false,
			wantQueue:   newQueueWithJob(t, userJob, false, depToNeglect),
		},
		{
			name:        "duplicate dependency",
			queue:       newQueueWithJob(t, userJob, false, depToResolve, depToResolve),
			wantExecute: true,
			wantQueue:   NewQueue[int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Reset the variable between tests
			calledExecute = false

			require.NoError(test.queue.Fulfill(context.Background(), depToResolve))
			require.Equal(test.wantExecute, calledExecute)
			require.Equal(test.wantQueue, test.queue)
		})
	}
}

func TestQueue_Abandon(t *testing.T) {
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
		queue         *Queue[int]
		wantCancelled bool
		wantQueue     *Queue[int]
	}{
		{
			name:          "no jobs",
			queue:         NewQueue[int](),
			wantCancelled: false,
			wantQueue:     NewQueue[int](),
		},
		{
			name:          "single dependency",
			queue:         newQueueWithJob(t, userJob, false, depToResolve),
			wantCancelled: true,
			wantQueue:     NewQueue[int](),
		},
		{
			name:          "non-existent dependency",
			queue:         newQueueWithJob(t, userJob, false, depToNeglect),
			wantCancelled: false,
			wantQueue:     newQueueWithJob(t, userJob, false, depToNeglect),
		},
		{
			name:          "incomplete dependencies",
			queue:         newQueueWithJob(t, userJob, false, depToResolve, depToNeglect),
			wantCancelled: false,
			wantQueue:     newQueueWithJob(t, userJob, true, depToNeglect),
		},
		{
			name:          "duplicate dependency",
			queue:         newQueueWithJob(t, userJob, false, depToResolve, depToResolve),
			wantCancelled: true,
			wantQueue:     NewQueue[int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Reset the variable between tests
			calledCancel = false

			require.NoError(test.queue.Abandon(context.Background(), depToResolve))
			require.Equal(test.wantCancelled, calledCancel)
			require.Equal(test.wantQueue, test.queue)
		})
	}
}
