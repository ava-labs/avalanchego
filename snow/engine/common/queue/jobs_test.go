// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"bytes"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

// Magic value that comes from the size in bytes of a serialized key-value bootstrap checkpoint in a database +
// the overhead of the key-value storage.
const bootstrapProgressCheckpointSize = 55

func testJob(t *testing.T, jobID ids.ID, executed *bool, parentID ids.ID, parentExecuted *bool) *TestJob {
	return &TestJob{
		T:   t,
		IDF: func() ids.ID { return jobID },
		MissingDependenciesF: func() (ids.Set, error) {
			if parentID != ids.Empty && !*parentExecuted {
				return ids.Set{parentID: struct{}{}}, nil
			}
			return ids.Set{}, nil
		},
		HasMissingDependenciesF: func() (bool, error) {
			if parentID != ids.Empty && !*parentExecuted {
				return true, nil
			}
			return false, nil
		},
		ExecuteF: func() error {
			if executed != nil {
				*executed = true
			}
			return nil
		},
		BytesF: func() []byte { return []byte{0} },
	}
}

// Test that creating a new queue can be created and that it is initially empty.
func TestNew(t *testing.T) {
	assert := assert.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := New(db, "", prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.SetParser(parser); err != nil {
		t.Fatal(err)
	}

	dbSize, err := database.Size(db)
	assert.NoError(err)
	assert.Zero(dbSize)
}

// Test that a job can be added to a queue, and then the job can be executed
// from the queue after a shutdown.
func TestPushAndExecute(t *testing.T) {
	assert := assert.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := New(db, "", prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.SetParser(parser); err != nil {
		t.Fatal(err)
	}

	jobID := ids.GenerateTestID()
	job := testJob(t, jobID, nil, ids.Empty, nil)
	has, err := jobs.Has(jobID)
	assert.NoError(err)
	assert.False(has)

	pushed, err := jobs.Push(job)
	assert.True(pushed)
	assert.NoError(err)

	has, err = jobs.Has(jobID)
	assert.NoError(err)
	assert.True(has)

	err = jobs.Commit()
	assert.NoError(err)

	jobs, err = New(db, "", prometheus.NewRegistry())
	assert.NoError(err)
	if err := jobs.SetParser(parser); err != nil {
		t.Fatal(err)
	}

	has, err = jobs.Has(jobID)
	assert.NoError(err)
	assert.True(has)

	hasNext, err := jobs.state.HasRunnableJob()
	assert.NoError(err)
	assert.True(hasNext)

	parser.ParseF = func(b []byte) (Job, error) {
		assert.Equal([]byte{0}, b)
		return job, nil
	}

	count, err := jobs.ExecuteAll(snow.DefaultConsensusContextTest(), &common.Halter{}, false)
	assert.NoError(err)
	assert.Equal(1, count)

	has, err = jobs.Has(jobID)
	assert.NoError(err)
	assert.False(has)

	hasNext, err = jobs.state.HasRunnableJob()
	assert.NoError(err)
	assert.False(hasNext)

	dbSize, err := database.Size(db)
	assert.NoError(err)
	assert.Equal(bootstrapProgressCheckpointSize, dbSize)
}

// Test that executing a job will cause a dependent job to be placed on to the
// ready queue
func TestRemoveDependency(t *testing.T) {
	assert := assert.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := New(db, "", prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.SetParser(parser); err != nil {
		t.Fatal(err)
	}

	job0ID, executed0 := ids.GenerateTestID(), false
	job1ID, executed1 := ids.GenerateTestID(), false

	job0 := testJob(t, job0ID, &executed0, ids.Empty, nil)
	job1 := testJob(t, job1ID, &executed1, job0ID, &executed0)
	job1.BytesF = func() []byte { return []byte{1} }

	pushed, err := jobs.Push(job1)
	assert.True(pushed)
	assert.NoError(err)

	hasNext, err := jobs.state.HasRunnableJob()
	assert.NoError(err)
	assert.False(hasNext)

	pushed, err = jobs.Push(job0)
	assert.True(pushed)
	assert.NoError(err)

	hasNext, err = jobs.state.HasRunnableJob()
	assert.NoError(err)
	assert.True(hasNext)

	parser.ParseF = func(b []byte) (Job, error) {
		switch {
		case bytes.Equal(b, []byte{0}):
			return job0, nil
		case bytes.Equal(b, []byte{1}):
			return job1, nil
		default:
			assert.FailNow("Unknown job")
			return nil, nil
		}
	}

	count, err := jobs.ExecuteAll(snow.DefaultConsensusContextTest(), &common.Halter{}, false)
	assert.NoError(err)
	assert.Equal(2, count)
	assert.True(executed0)
	assert.True(executed1)

	hasNext, err = jobs.state.HasRunnableJob()
	assert.NoError(err)
	assert.False(hasNext)

	dbSize, err := database.Size(db)
	assert.NoError(err)
	assert.Equal(bootstrapProgressCheckpointSize, dbSize)
}

// Test that a job that is ready to be executed can only be added once
func TestDuplicatedExecutablePush(t *testing.T) {
	assert := assert.New(t)

	db := memdb.New()

	jobs, err := New(db, "", prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}

	jobID := ids.GenerateTestID()
	job := testJob(t, jobID, nil, ids.Empty, nil)

	pushed, err := jobs.Push(job)
	assert.True(pushed)
	assert.NoError(err)

	pushed, err = jobs.Push(job)
	assert.False(pushed)
	assert.NoError(err)

	err = jobs.Commit()
	assert.NoError(err)

	jobs, err = New(db, "", prometheus.NewRegistry())
	assert.NoError(err)

	pushed, err = jobs.Push(job)
	assert.False(pushed)
	assert.NoError(err)
}

// Test that a job that isn't ready to be executed can only be added once
func TestDuplicatedNotExecutablePush(t *testing.T) {
	assert := assert.New(t)

	db := memdb.New()

	jobs, err := New(db, "", prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}

	job0ID, executed0 := ids.GenerateTestID(), false
	job1ID := ids.GenerateTestID()
	job1 := testJob(t, job1ID, nil, job0ID, &executed0)

	pushed, err := jobs.Push(job1)
	assert.True(pushed)
	assert.NoError(err)

	pushed, err = jobs.Push(job1)
	assert.False(pushed)
	assert.NoError(err)

	err = jobs.Commit()
	assert.NoError(err)

	jobs, err = New(db, "", prometheus.NewRegistry())
	assert.NoError(err)

	pushed, err = jobs.Push(job1)
	assert.False(pushed)
	assert.NoError(err)
}

func TestMissingJobs(t *testing.T) {
	assert := assert.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := NewWithMissing(db, "", prometheus.NewRegistry())
	assert.NoError(err)
	if err := jobs.SetParser(parser); err != nil {
		t.Fatal(err)
	}

	job0ID := ids.GenerateTestID()
	job1ID := ids.GenerateTestID()

	jobs.AddMissingID(job0ID)
	jobs.AddMissingID(job1ID)

	err = jobs.Commit()
	assert.NoError(err)

	numMissingIDs := jobs.NumMissingIDs()
	assert.Equal(2, numMissingIDs)

	missingIDSet := ids.Set{}
	missingIDSet.Add(jobs.MissingIDs()...)

	containsJob0ID := missingIDSet.Contains(job0ID)
	assert.True(containsJob0ID)

	containsJob1ID := missingIDSet.Contains(job1ID)
	assert.True(containsJob1ID)

	jobs.RemoveMissingID(job1ID)

	err = jobs.Commit()
	assert.NoError(err)

	jobs, err = NewWithMissing(db, "", prometheus.NewRegistry())
	assert.NoError(err)
	if err := jobs.SetParser(parser); err != nil {
		t.Fatal(err)
	}

	missingIDSet = ids.Set{}
	missingIDSet.Add(jobs.MissingIDs()...)

	containsJob0ID = missingIDSet.Contains(job0ID)
	assert.True(containsJob0ID)

	containsJob1ID = missingIDSet.Contains(job1ID)
	assert.False(containsJob1ID)
}

func TestHandleJobWithMissingDependencyOnRunnableStack(t *testing.T) {
	assert := assert.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := NewWithMissing(db, "", prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.SetParser(parser); err != nil {
		t.Fatal(err)
	}

	job0ID, executed0 := ids.GenerateTestID(), false
	job1ID, executed1 := ids.GenerateTestID(), false
	job0 := testJob(t, job0ID, &executed0, ids.Empty, nil)
	job1 := testJob(t, job1ID, &executed1, job0ID, &executed0)
	job1.ExecuteF = func() error { return database.ErrClosed } // job1 fails to execute the first time due to a closed database
	job1.BytesF = func() []byte { return []byte{1} }

	pushed, err := jobs.Push(job1)
	assert.True(pushed)
	assert.NoError(err)

	hasNext, err := jobs.state.HasRunnableJob()
	assert.NoError(err)
	assert.False(hasNext)

	pushed, err = jobs.Push(job0)
	assert.True(pushed)
	assert.NoError(err)

	hasNext, err = jobs.state.HasRunnableJob()
	assert.NoError(err)
	assert.True(hasNext)

	parser.ParseF = func(b []byte) (Job, error) {
		switch {
		case bytes.Equal(b, []byte{0}):
			return job0, nil
		case bytes.Equal(b, []byte{1}):
			return job1, nil
		default:
			assert.FailNow("Unknown job")
			return nil, nil
		}
	}

	_, err = jobs.ExecuteAll(snow.DefaultConsensusContextTest(), &common.Halter{}, false)
	// Assert that the database closed error on job1 causes ExecuteAll
	// to fail in the middle of execution.
	assert.Error(err)
	assert.True(executed0)
	assert.False(executed1)

	executed0 = false
	job1.ExecuteF = func() error { executed1 = true; return nil } // job1 succeeds the second time

	// Create jobs queue from the same database and ensure that the jobs queue
	// recovers correctly.
	jobs, err = NewWithMissing(db, "", prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.SetParser(parser); err != nil {
		t.Fatal(err)
	}

	missingIDs := jobs.MissingIDs()
	assert.Equal(1, len(missingIDs))

	assert.Equal(missingIDs[0], job0.ID())

	pushed, err = jobs.Push(job0)
	assert.NoError(err)
	assert.True(pushed)

	hasNext, err = jobs.state.HasRunnableJob()
	assert.NoError(err)
	assert.True(hasNext)

	count, err := jobs.ExecuteAll(snow.DefaultConsensusContextTest(), &common.Halter{}, false)
	assert.NoError(err)
	assert.Equal(2, count)
	assert.True(executed1)
}

func TestInitializeNumJobs(t *testing.T) {
	assert := assert.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := NewWithMissing(db, "", prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.SetParser(parser); err != nil {
		t.Fatal(err)
	}

	job0ID := ids.GenerateTestID()
	job1ID := ids.GenerateTestID()

	job0 := &TestJob{
		T: t,

		IDF:                     func() ids.ID { return job0ID },
		MissingDependenciesF:    func() (ids.Set, error) { return nil, nil },
		HasMissingDependenciesF: func() (bool, error) { return false, nil },
		BytesF:                  func() []byte { return []byte{0} },
	}
	job1 := &TestJob{
		T: t,

		IDF:                     func() ids.ID { return job1ID },
		MissingDependenciesF:    func() (ids.Set, error) { return nil, nil },
		HasMissingDependenciesF: func() (bool, error) { return false, nil },
		BytesF:                  func() []byte { return []byte{1} },
	}

	pushed, err := jobs.Push(job0)
	assert.True(pushed)
	assert.NoError(err)
	assert.EqualValues(1, jobs.state.numJobs)

	pushed, err = jobs.Push(job1)
	assert.True(pushed)
	assert.NoError(err)
	assert.EqualValues(2, jobs.state.numJobs)

	err = jobs.Commit()
	assert.NoError(err)

	err = database.Clear(jobs.state.metadataDB, jobs.state.metadataDB)
	assert.NoError(err)

	err = jobs.Commit()
	assert.NoError(err)

	jobs, err = NewWithMissing(db, "", prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	assert.EqualValues(2, jobs.state.numJobs)
}

func TestClearAll(t *testing.T) {
	assert := assert.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := NewWithMissing(db, "", prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if err := jobs.SetParser(parser); err != nil {
		t.Fatal(err)
	}
	job0ID, executed0 := ids.GenerateTestID(), false
	job1ID, executed1 := ids.GenerateTestID(), false
	job0 := testJob(t, job0ID, &executed0, ids.Empty, nil)
	job1 := testJob(t, job1ID, &executed1, job0ID, &executed0)
	job1.BytesF = func() []byte { return []byte{1} }

	pushed, err := jobs.Push(job0)
	assert.NoError(err)
	assert.True(pushed)

	pushed, err = jobs.Push(job1)
	assert.True(pushed)
	assert.NoError(err)

	parser.ParseF = func(b []byte) (Job, error) {
		switch {
		case bytes.Equal(b, []byte{0}):
			return job0, nil
		case bytes.Equal(b, []byte{1}):
			return job1, nil
		default:
			assert.FailNow("Unknown job")
			return nil, nil
		}
	}

	assert.NoError(jobs.Clear())
	hasJob0, err := jobs.Has(job0.ID())
	assert.NoError(err)
	assert.False(hasJob0)
	hasJob1, err := jobs.Has(job1.ID())
	assert.NoError(err)
	assert.False(hasJob1)
}
