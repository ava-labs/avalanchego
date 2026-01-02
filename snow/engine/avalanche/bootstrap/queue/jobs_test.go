// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"bytes"
	"context"
	"math"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Magic value that comes from the size in bytes of a serialized key-value bootstrap checkpoint in a database +
// the overhead of the key-value storage.
const bootstrapProgressCheckpointSize = 55

func testJob(t *testing.T, jobID ids.ID, executed *bool, parentID ids.ID, parentExecuted *bool) *TestJob {
	return &TestJob{
		T: t,
		IDF: func() ids.ID {
			return jobID
		},
		MissingDependenciesF: func(context.Context) (set.Set[ids.ID], error) {
			if parentID != ids.Empty && !*parentExecuted {
				return set.Of(parentID), nil
			}
			return set.Set[ids.ID]{}, nil
		},
		HasMissingDependenciesF: func(context.Context) (bool, error) {
			if parentID != ids.Empty && !*parentExecuted {
				return true, nil
			}
			return false, nil
		},
		ExecuteF: func(context.Context) error {
			if executed != nil {
				*executed = true
			}
			return nil
		},
		BytesF: func() []byte {
			return []byte{0}
		},
	}
}

// Test that creating a new queue can be created and that it is initially empty.
func TestNew(t *testing.T) {
	require := require.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := New(db, "", prometheus.NewRegistry())
	require.NoError(err)
	require.NoError(jobs.SetParser(parser))

	dbSize, err := database.Size(db)
	require.NoError(err)
	require.Zero(dbSize)
}

// Test that a job can be added to a queue, and then the job can be executed
// from the queue after a shutdown.
func TestPushAndExecute(t *testing.T) {
	require := require.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := New(db, "", prometheus.NewRegistry())
	require.NoError(err)
	require.NoError(jobs.SetParser(parser))

	jobID := ids.GenerateTestID()
	job := testJob(t, jobID, nil, ids.Empty, nil)
	has, err := jobs.Has(jobID)
	require.NoError(err)
	require.False(has)

	pushed, err := jobs.Push(t.Context(), job)
	require.True(pushed)
	require.NoError(err)

	has, err = jobs.Has(jobID)
	require.NoError(err)
	require.True(has)

	require.NoError(jobs.Commit())

	jobs, err = New(db, "", prometheus.NewRegistry())
	require.NoError(err)
	require.NoError(jobs.SetParser(parser))

	has, err = jobs.Has(jobID)
	require.NoError(err)
	require.True(has)

	hasNext, err := jobs.state.HasRunnableJob()
	require.NoError(err)
	require.True(hasNext)

	parser.ParseF = func(_ context.Context, b []byte) (Job, error) {
		require.Equal([]byte{0}, b)
		return job, nil
	}

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	count, err := jobs.ExecuteAll(t.Context(), snowtest.ConsensusContext(snowCtx), &common.Halter{}, false)
	require.NoError(err)
	require.Equal(1, count)

	has, err = jobs.Has(jobID)
	require.NoError(err)
	require.False(has)

	hasNext, err = jobs.state.HasRunnableJob()
	require.NoError(err)
	require.False(hasNext)

	dbSize, err := database.Size(db)
	require.NoError(err)
	require.Equal(bootstrapProgressCheckpointSize, dbSize)
}

// Test that executing a job will cause a dependent job to be placed on to the
// ready queue
func TestRemoveDependency(t *testing.T) {
	require := require.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := New(db, "", prometheus.NewRegistry())
	require.NoError(err)
	require.NoError(jobs.SetParser(parser))

	job0ID, executed0 := ids.GenerateTestID(), false
	job1ID, executed1 := ids.GenerateTestID(), false

	job0 := testJob(t, job0ID, &executed0, ids.Empty, nil)
	job1 := testJob(t, job1ID, &executed1, job0ID, &executed0)
	job1.BytesF = func() []byte {
		return []byte{1}
	}

	pushed, err := jobs.Push(t.Context(), job1)
	require.True(pushed)
	require.NoError(err)

	hasNext, err := jobs.state.HasRunnableJob()
	require.NoError(err)
	require.False(hasNext)

	pushed, err = jobs.Push(t.Context(), job0)
	require.True(pushed)
	require.NoError(err)

	hasNext, err = jobs.state.HasRunnableJob()
	require.NoError(err)
	require.True(hasNext)

	parser.ParseF = func(_ context.Context, b []byte) (Job, error) {
		switch {
		case bytes.Equal(b, []byte{0}):
			return job0, nil
		case bytes.Equal(b, []byte{1}):
			return job1, nil
		default:
			require.FailNow("Unknown job")
			return nil, nil
		}
	}

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	count, err := jobs.ExecuteAll(t.Context(), snowtest.ConsensusContext(snowCtx), &common.Halter{}, false)
	require.NoError(err)
	require.Equal(2, count)
	require.True(executed0)
	require.True(executed1)

	hasNext, err = jobs.state.HasRunnableJob()
	require.NoError(err)
	require.False(hasNext)

	dbSize, err := database.Size(db)
	require.NoError(err)
	require.Equal(bootstrapProgressCheckpointSize, dbSize)
}

// Test that a job that is ready to be executed can only be added once
func TestDuplicatedExecutablePush(t *testing.T) {
	require := require.New(t)

	db := memdb.New()

	jobs, err := New(db, "", prometheus.NewRegistry())
	require.NoError(err)

	jobID := ids.GenerateTestID()
	job := testJob(t, jobID, nil, ids.Empty, nil)

	pushed, err := jobs.Push(t.Context(), job)
	require.True(pushed)
	require.NoError(err)

	pushed, err = jobs.Push(t.Context(), job)
	require.False(pushed)
	require.NoError(err)

	require.NoError(jobs.Commit())

	jobs, err = New(db, "", prometheus.NewRegistry())
	require.NoError(err)

	pushed, err = jobs.Push(t.Context(), job)
	require.False(pushed)
	require.NoError(err)
}

// Test that a job that isn't ready to be executed can only be added once
func TestDuplicatedNotExecutablePush(t *testing.T) {
	require := require.New(t)

	db := memdb.New()

	jobs, err := New(db, "", prometheus.NewRegistry())
	require.NoError(err)

	job0ID, executed0 := ids.GenerateTestID(), false
	job1ID := ids.GenerateTestID()
	job1 := testJob(t, job1ID, nil, job0ID, &executed0)

	pushed, err := jobs.Push(t.Context(), job1)
	require.True(pushed)
	require.NoError(err)

	pushed, err = jobs.Push(t.Context(), job1)
	require.False(pushed)
	require.NoError(err)

	require.NoError(jobs.Commit())

	jobs, err = New(db, "", prometheus.NewRegistry())
	require.NoError(err)

	pushed, err = jobs.Push(t.Context(), job1)
	require.False(pushed)
	require.NoError(err)
}

func TestMissingJobs(t *testing.T) {
	require := require.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := NewWithMissing(db, "", prometheus.NewRegistry())
	require.NoError(err)
	require.NoError(jobs.SetParser(t.Context(), parser))

	job0ID := ids.GenerateTestID()
	job1ID := ids.GenerateTestID()

	jobs.AddMissingID(job0ID)
	jobs.AddMissingID(job1ID)

	require.NoError(jobs.Commit())

	numMissingIDs := jobs.NumMissingIDs()
	require.Equal(2, numMissingIDs)

	missingIDSet := set.Of(jobs.MissingIDs()...)

	containsJob0ID := missingIDSet.Contains(job0ID)
	require.True(containsJob0ID)

	containsJob1ID := missingIDSet.Contains(job1ID)
	require.True(containsJob1ID)

	jobs.RemoveMissingID(job1ID)

	require.NoError(jobs.Commit())

	jobs, err = NewWithMissing(db, "", prometheus.NewRegistry())
	require.NoError(err)
	require.NoError(jobs.SetParser(t.Context(), parser))

	missingIDSet = set.Of(jobs.MissingIDs()...)

	containsJob0ID = missingIDSet.Contains(job0ID)
	require.True(containsJob0ID)

	containsJob1ID = missingIDSet.Contains(job1ID)
	require.False(containsJob1ID)
}

func TestHandleJobWithMissingDependencyOnRunnableStack(t *testing.T) {
	require := require.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := NewWithMissing(db, "", prometheus.NewRegistry())
	require.NoError(err)
	require.NoError(jobs.SetParser(t.Context(), parser))

	job0ID, executed0 := ids.GenerateTestID(), false
	job1ID, executed1 := ids.GenerateTestID(), false
	job0 := testJob(t, job0ID, &executed0, ids.Empty, nil)
	job1 := testJob(t, job1ID, &executed1, job0ID, &executed0)

	// job1 fails to execute the first time due to a closed database
	job1.ExecuteF = func(context.Context) error {
		return database.ErrClosed
	}
	job1.BytesF = func() []byte {
		return []byte{1}
	}

	pushed, err := jobs.Push(t.Context(), job1)
	require.True(pushed)
	require.NoError(err)

	hasNext, err := jobs.state.HasRunnableJob()
	require.NoError(err)
	require.False(hasNext)

	pushed, err = jobs.Push(t.Context(), job0)
	require.True(pushed)
	require.NoError(err)

	hasNext, err = jobs.state.HasRunnableJob()
	require.NoError(err)
	require.True(hasNext)

	parser.ParseF = func(_ context.Context, b []byte) (Job, error) {
		switch {
		case bytes.Equal(b, []byte{0}):
			return job0, nil
		case bytes.Equal(b, []byte{1}):
			return job1, nil
		default:
			require.FailNow("Unknown job")
			return nil, nil
		}
	}

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	_, err = jobs.ExecuteAll(t.Context(), snowtest.ConsensusContext(snowCtx), &common.Halter{}, false)
	// Assert that the database closed error on job1 causes ExecuteAll
	// to fail in the middle of execution.
	require.ErrorIs(err, database.ErrClosed)
	require.True(executed0)
	require.False(executed1)

	executed0 = false
	job1.ExecuteF = func(context.Context) error {
		executed1 = true // job1 succeeds the second time
		return nil
	}

	// Create jobs queue from the same database and ensure that the jobs queue
	// recovers correctly.
	jobs, err = NewWithMissing(db, "", prometheus.NewRegistry())
	require.NoError(err)
	require.NoError(jobs.SetParser(t.Context(), parser))

	missingIDs := jobs.MissingIDs()
	require.Len(missingIDs, 1)

	require.Equal(missingIDs[0], job0.ID())

	pushed, err = jobs.Push(t.Context(), job0)
	require.NoError(err)
	require.True(pushed)

	hasNext, err = jobs.state.HasRunnableJob()
	require.NoError(err)
	require.True(hasNext)

	count, err := jobs.ExecuteAll(t.Context(), snowtest.ConsensusContext(snowCtx), &common.Halter{}, false)
	require.NoError(err)
	require.Equal(2, count)
	require.True(executed1)
}

func TestInitializeNumJobs(t *testing.T) {
	require := require.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := NewWithMissing(db, "", prometheus.NewRegistry())
	require.NoError(err)
	require.NoError(jobs.SetParser(t.Context(), parser))

	job0ID := ids.GenerateTestID()
	job1ID := ids.GenerateTestID()

	job0 := &TestJob{
		T: t,

		IDF: func() ids.ID {
			return job0ID
		},
		MissingDependenciesF: func(context.Context) (set.Set[ids.ID], error) {
			return nil, nil
		},
		HasMissingDependenciesF: func(context.Context) (bool, error) {
			return false, nil
		},
		BytesF: func() []byte {
			return []byte{0}
		},
	}
	job1 := &TestJob{
		T: t,

		IDF: func() ids.ID {
			return job1ID
		},
		MissingDependenciesF: func(context.Context) (set.Set[ids.ID], error) {
			return nil, nil
		},
		HasMissingDependenciesF: func(context.Context) (bool, error) {
			return false, nil
		},
		BytesF: func() []byte {
			return []byte{1}
		},
	}

	pushed, err := jobs.Push(t.Context(), job0)
	require.True(pushed)
	require.NoError(err)
	require.Equal(uint64(1), jobs.state.numJobs)

	pushed, err = jobs.Push(t.Context(), job1)
	require.True(pushed)
	require.NoError(err)
	require.Equal(uint64(2), jobs.state.numJobs)

	require.NoError(jobs.Commit())
	require.NoError(database.Clear(jobs.state.metadataDB, math.MaxInt))
	require.NoError(jobs.Commit())

	jobs, err = NewWithMissing(db, "", prometheus.NewRegistry())
	require.NoError(err)
	require.Equal(uint64(2), jobs.state.numJobs)
}

func TestClearAll(t *testing.T) {
	require := require.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := NewWithMissing(db, "", prometheus.NewRegistry())
	require.NoError(err)
	require.NoError(jobs.SetParser(t.Context(), parser))
	job0ID, executed0 := ids.GenerateTestID(), false
	job1ID, executed1 := ids.GenerateTestID(), false
	job0 := testJob(t, job0ID, &executed0, ids.Empty, nil)
	job1 := testJob(t, job1ID, &executed1, job0ID, &executed0)
	job1.BytesF = func() []byte {
		return []byte{1}
	}

	pushed, err := jobs.Push(t.Context(), job0)
	require.NoError(err)
	require.True(pushed)

	pushed, err = jobs.Push(t.Context(), job1)
	require.True(pushed)
	require.NoError(err)

	parser.ParseF = func(_ context.Context, b []byte) (Job, error) {
		switch {
		case bytes.Equal(b, []byte{0}):
			return job0, nil
		case bytes.Equal(b, []byte{1}):
			return job1, nil
		default:
			require.FailNow("Unknown job")
			return nil, nil
		}
	}

	require.NoError(jobs.Clear())
	hasJob0, err := jobs.Has(job0.ID())
	require.NoError(err)
	require.False(hasJob0)
	hasJob1, err := jobs.Has(job1.ID())
	require.NoError(err)
	require.False(hasJob1)
}
