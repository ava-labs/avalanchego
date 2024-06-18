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

func newQueueWithJob[T comparable](t *testing.T, job Job, dependencies ...T) *Queue[T] {
	q := NewQueue[T]()
	require.NoError(t, q.Register(context.Background(), job, dependencies...))
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
		name          string
		queue         *Queue[int]
		dependencies  []int
		shouldExecute bool
		expectedLen   int
		expectedQueue *Queue[int]
	}{
		{
			name:          "no dependencies",
			queue:         NewQueue[int](),
			dependencies:  nil,
			shouldExecute: true,
			expectedLen:   0,
			expectedQueue: NewQueue[int](),
		},
		{
			name:          "one dependency",
			queue:         NewQueue[int](),
			dependencies:  []int{1},
			shouldExecute: false,
			expectedLen:   1,
			expectedQueue: &Queue[int]{
				jobs: map[int][]*job[int]{
					1: {
						{
							dependencies: set.Of(1),
							job:          userJob,
						},
					},
				},
			},
		},
		{
			name:          "two dependencies",
			queue:         NewQueue[int](),
			dependencies:  []int{1, 2},
			shouldExecute: false,
			expectedLen:   2,
			expectedQueue: &Queue[int]{
				jobs: map[int][]*job[int]{
					1: {
						{
							dependencies: set.Of(1, 2),
							job:          userJob,
						},
					},
					2: {
						{
							dependencies: set.Of(1, 2),
							job:          userJob,
						},
					},
				},
			},
		},
		{
			name: "additional dependency",
			queue: &Queue[int]{
				jobs: map[int][]*job[int]{
					1: {
						{
							dependencies: set.Of(1),
							job:          userJob,
						},
					},
				},
			},
			dependencies:  []int{1},
			shouldExecute: false,
			expectedLen:   1,
			expectedQueue: &Queue[int]{
				jobs: map[int][]*job[int]{
					1: {
						{
							dependencies: set.Of(1),
							job:          userJob,
						},
						{
							dependencies: set.Of(1),
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
			require.Equal(test.expectedLen, test.queue.Len())
			require.Equal(test.shouldExecute, calledExecute)
			require.Equal(test.expectedQueue, test.queue)
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
		name          string
		queue         *Queue[int]
		shouldExecute bool
		expectedQueue *Queue[int]
	}{
		{
			name:          "no jobs",
			queue:         NewQueue[int](),
			shouldExecute: false,
			expectedQueue: NewQueue[int](),
		},
		{
			name:          "single dependency",
			queue:         newQueueWithJob(t, userJob, 1),
			shouldExecute: true,
			expectedQueue: NewQueue[int](),
		},
		{
			name:          "non-existent dependency",
			queue:         newQueueWithJob(t, userJob, 2),
			shouldExecute: false,
			expectedQueue: newQueueWithJob(t, userJob, 2),
		},
		{
			name:          "incomplete dependencies",
			queue:         newQueueWithJob(t, userJob, 1, 2),
			shouldExecute: false,
			expectedQueue: newQueueWithJob(t, userJob, 2),
		},
		{
			name:          "duplicate dependency",
			queue:         newQueueWithJob(t, userJob, 1, 1),
			shouldExecute: true,
			expectedQueue: NewQueue[int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Reset the variable between tests
			calledExecute = false

			require.NoError(test.queue.Fulfill(context.Background(), 1))
			require.Equal(test.shouldExecute, calledExecute)
			require.Equal(test.expectedQueue, test.queue)
		})
	}
}

func TestQueue_Abandon(t *testing.T) {
	var calledAbandon bool
	userJob := &testJob{
		execute: func(context.Context) error {
			return errUnexpectedInvocation
		},
		cancel: func(context.Context) error {
			if calledAbandon {
				return errDuplicateInvocation
			}
			calledAbandon = true
			return nil
		},
	}

	tests := []struct {
		name          string
		queue         *Queue[int]
		shouldCancel  bool
		expectedQueue *Queue[int]
	}{
		{
			name:          "no jobs",
			queue:         NewQueue[int](),
			shouldCancel:  false,
			expectedQueue: NewQueue[int](),
		},
		{
			name:          "single dependency",
			queue:         newQueueWithJob(t, userJob, 1),
			shouldCancel:  true,
			expectedQueue: NewQueue[int](),
		},
		{
			name:          "non-existent dependency",
			queue:         newQueueWithJob(t, userJob, 2),
			shouldCancel:  false,
			expectedQueue: newQueueWithJob(t, userJob, 2),
		},
		{
			name:          "incomplete dependencies",
			queue:         newQueueWithJob(t, userJob, 1, 2),
			shouldCancel:  true,
			expectedQueue: newQueueWithJob(t, nil, 2),
		},
		{
			name:          "duplicate dependency",
			queue:         newQueueWithJob(t, userJob, 1, 1),
			shouldCancel:  true,
			expectedQueue: NewQueue[int](),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Reset the variable between tests
			calledAbandon = false

			require.NoError(test.queue.Abandon(context.Background(), 1))
			require.Equal(test.shouldCancel, calledAbandon)
			require.Equal(test.expectedQueue, test.queue)
		})
	}
}
