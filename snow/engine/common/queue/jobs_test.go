// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"bytes"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

// Test that creating a new queue can be created and that it is initially empty.
func TestNew(t *testing.T) {
	assert := assert.New(t)

	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := New(db, "", prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	jobs.SetParser(parser)

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
	jobs.SetParser(parser)

	jobID := ids.GenerateTestID()
	job := &TestJob{
		T: t,

		IDF:                  func() ids.ID { return jobID },
		MissingDependenciesF: func() (ids.Set, error) { return ids.Set{}, nil },
		ExecuteF:             func() error { return nil },
		BytesF:               func() []byte { return []byte{0} },
	}

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
	jobs.SetParser(parser)

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

	count, err := jobs.ExecuteAll(snow.DefaultContextTest())
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
	assert.Zero(dbSize)
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
	jobs.SetParser(parser)

	job0ID := ids.GenerateTestID()
	executed0 := false
	job1ID := ids.GenerateTestID()
	executed1 := false

	job1 := &TestJob{
		T: t,

		IDF:                  func() ids.ID { return job1ID },
		MissingDependenciesF: func() (ids.Set, error) { return ids.Set{job0ID: true}, nil },
		ExecuteF:             func() error { executed1 = true; return nil },
		BytesF:               func() []byte { return []byte{1} },
	}
	job0 := &TestJob{
		T: t,

		IDF:                  func() ids.ID { return job0ID },
		MissingDependenciesF: func() (ids.Set, error) { return ids.Set{}, nil },
		ExecuteF: func() error {
			executed0 = true
			job1.MissingDependenciesF = func() (ids.Set, error) {
				return ids.Set{}, nil
			}
			return nil
		},
		BytesF: func() []byte { return []byte{0} },
	}

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

	count, err := jobs.ExecuteAll(snow.DefaultContextTest())
	assert.NoError(err)
	assert.Equal(2, count)
	assert.True(executed0)
	assert.True(executed1)

	hasNext, err = jobs.state.HasRunnableJob()
	assert.NoError(err)
	assert.False(hasNext)

	dbSize, err := database.Size(db)
	assert.NoError(err)
	assert.Zero(dbSize)
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
	job := &TestJob{
		T: t,

		IDF:                  func() ids.ID { return jobID },
		MissingDependenciesF: func() (ids.Set, error) { return ids.Set{}, nil },
		ExecuteF:             func() error { return nil },
		BytesF:               func() []byte { return []byte{0} },
	}

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

	job0ID := ids.GenerateTestID()
	job1ID := ids.GenerateTestID()
	job1 := &TestJob{
		T: t,

		IDF:                  func() ids.ID { return job1ID },
		MissingDependenciesF: func() (ids.Set, error) { return ids.Set{job0ID: true}, nil },
		ExecuteF:             func() error { return nil },
		BytesF:               func() []byte { return []byte{1} },
	}

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
	jobs.SetParser(parser)

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
	jobs.SetParser(parser)

	missingIDSet = ids.Set{}
	missingIDSet.Add(jobs.MissingIDs()...)

	containsJob0ID = missingIDSet.Contains(job0ID)
	assert.True(containsJob0ID)

	containsJob1ID = missingIDSet.Contains(job1ID)
	assert.False(containsJob1ID)
}
