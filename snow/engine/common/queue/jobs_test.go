// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"bytes"
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
)

// Test that creating a new queue can be created and that it is initially empty.
func TestNew(t *testing.T) {
	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := New(db)
	if err != nil {
		t.Fatal(err)
	}

	jobs.SetParser(parser)

	if hasNext, err := jobs.HasNext(); err != nil {
		t.Fatal(err)
	} else if hasNext {
		t.Fatalf("Haven't pushed anything yet, shouldn't be able to pop")
	}
}

// Test that a job can be added to a queue, and then the job can be removed from
// the queue after a shutdown.
func TestPushPop(t *testing.T) {
	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := New(db)
	if err != nil {
		t.Fatal(err)
	}

	jobs.SetParser(parser)

	id := ids.Empty.Prefix(0)
	job := &TestJob{
		T: t,

		IDF:                  func() ids.ID { return id },
		MissingDependenciesF: func() (ids.Set, error) { return ids.Set{}, nil },
		ExecuteF:             func() error { return nil },
		BytesF:               func() []byte { return []byte{0} },
	}

	if err := jobs.Push(job); err != nil {
		t.Fatal(err)
	}

	if err := jobs.Commit(); err != nil {
		t.Fatal(err)
	}

	jobs, err = New(db)
	if err != nil {
		t.Fatal(err)
	}

	jobs.SetParser(parser)

	if hasNext, err := jobs.HasNext(); err != nil {
		t.Fatal(err)
	} else if !hasNext {
		t.Fatalf("Should have a container ready to pop")
	}

	parser.ParseF = func(b []byte) (Job, error) {
		if !bytes.Equal(b, []byte{0}) {
			t.Fatalf("Unknown job")
		}
		return job, nil
	}

	returnedBlockable, err := jobs.Pop()
	if err != nil {
		t.Fatal(err)
	}

	if returnedBlockable != job {
		t.Fatalf("Returned wrong job")
	}

	if hasNext, err := jobs.HasNext(); err != nil {
		t.Fatal(err)
	} else if hasNext {
		t.Fatalf("Shouldn't have a container ready to pop")
	}
}

// Test that executing a job will cause a dependent job to be placed on to the
// ready queue
func TestExecute(t *testing.T) {
	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := New(db)
	if err != nil {
		t.Fatal(err)
	}

	jobs.SetParser(parser)

	id0 := ids.Empty.Prefix(0)
	executed0 := new(bool)
	job0 := &TestJob{
		T: t,

		IDF:                  func() ids.ID { return id0 },
		MissingDependenciesF: func() (ids.Set, error) { return ids.Set{}, nil },
		ExecuteF:             func() error { *executed0 = true; return nil },
		BytesF:               func() []byte { return []byte{0} },
	}

	id1 := ids.Empty.Prefix(1)
	executed1 := new(bool)
	job1 := &TestJob{
		T: t,

		IDF:                  func() ids.ID { return id1 },
		MissingDependenciesF: func() (ids.Set, error) { return ids.Set{id0: true}, nil },
		ExecuteF:             func() error { *executed1 = true; return nil },
		BytesF:               func() []byte { return []byte{1} },
	}

	if err := jobs.Push(job0); err != nil {
		t.Fatal(err)
	}

	if err := jobs.Push(job1); err != nil {
		t.Fatal(err)
	}

	if hasNext, err := jobs.HasNext(); err != nil {
		t.Fatal(err)
	} else if !hasNext {
		t.Fatalf("Should have a container ready to pop")
	}

	parser.ParseF = func(b []byte) (Job, error) {
		if !bytes.Equal(b, []byte{0}) {
			t.Fatalf("Unknown job")
		}
		return job0, nil
	}

	returnedBlockable, err := jobs.Pop()
	if err != nil {
		t.Fatal(err)
	}

	parser.ParseF = nil

	if returnedBlockable != job0 {
		t.Fatalf("Returned wrong job")
	}

	job1.MissingDependenciesF = func() (ids.Set, error) { return ids.Set{}, nil }
	parser.ParseF = func(b []byte) (Job, error) {
		if !bytes.Equal(b, []byte{1}) {
			t.Fatalf("Unknown job")
		}
		return job1, nil
	}

	if err := jobs.Execute(job0); err != nil {
		t.Fatal(err)
	}

	if !*executed0 {
		t.Fatalf("Should have executed the container")
	}

	if hasNext, err := jobs.HasNext(); err != nil {
		t.Fatal(err)
	} else if !hasNext {
		t.Fatalf("Should have a container ready to pop")
	}
}

// Test that a job that is ready to be executed can only be added once
func TestDuplicatedExecutablePush(t *testing.T) {
	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := New(db)
	if err != nil {
		t.Fatal(err)
	}

	jobs.SetParser(parser)

	id := ids.Empty.Prefix(0)
	job := &TestJob{
		T: t,

		IDF:                  func() ids.ID { return id },
		MissingDependenciesF: func() (ids.Set, error) { return ids.Set{}, nil },
		ExecuteF:             func() error { return nil },
		BytesF:               func() []byte { return []byte{0} },
	}

	if err := jobs.Push(job); err != nil {
		t.Fatal(err)
	}

	if err := jobs.Push(job); err == nil {
		t.Fatalf("Should have failed on push")
	}

	if err := jobs.Commit(); err != nil {
		t.Fatal(err)
	}

	jobs, err = New(db)
	if err != nil {
		t.Fatal(err)
	}

	jobs.SetParser(parser)

	if hasNext, err := jobs.HasNext(); err != nil {
		t.Fatal(err)
	} else if !hasNext {
		t.Fatalf("Should have a container ready to pop")
	}

	parser.ParseF = func(b []byte) (Job, error) {
		if !bytes.Equal(b, []byte{0}) {
			t.Fatalf("Unknown job")
		}
		return job, nil
	}

	returnedBlockable, err := jobs.Pop()
	if err != nil {
		t.Fatal(err)
	}

	if returnedBlockable != job {
		t.Fatalf("Returned wrong job")
	}

	if hasNext, err := jobs.HasNext(); err != nil {
		t.Fatal(err)
	} else if hasNext {
		t.Fatalf("Shouldn't have a container ready to pop")
	}
}

// Test that a job that isn't ready to be executed can only be added once
func TestDuplicatedNotExecutablePush(t *testing.T) {
	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := New(db)
	if err != nil {
		t.Fatal(err)
	}

	jobs.SetParser(parser)

	id0 := ids.Empty.Prefix(0)
	id1 := ids.Empty.Prefix(1)
	job1 := &TestJob{
		T: t,

		IDF: func() ids.ID { return id1 },
		MissingDependenciesF: func() (ids.Set, error) {
			s := ids.Set{}
			s.Add(id0)
			return s, nil
		},
		ExecuteF: func() error { return nil },
		BytesF:   func() []byte { return []byte{1} },
	}
	job0 := &TestJob{
		T: t,

		IDF:                  func() ids.ID { return id0 },
		MissingDependenciesF: func() (ids.Set, error) { return ids.Set{}, nil },
		ExecuteF: func() error {
			job1.MissingDependenciesF = func() (ids.Set, error) { return ids.Set{}, nil }
			return nil
		},
		BytesF: func() []byte { return []byte{0} },
	}

	if err := jobs.Push(job1); err != nil {
		t.Fatal(err)
	}

	if err := jobs.Push(job1); err == nil {
		t.Fatalf("should have errored on pushing a duplicate job")
	}

	if err := jobs.Commit(); err != nil {
		t.Fatal(err)
	}

	jobs, err = New(db)
	if err != nil {
		t.Fatal(err)
	}

	jobs.SetParser(parser)

	if err := jobs.Push(job0); err != nil {
		t.Fatal(err)
	}

	if hasNext, err := jobs.HasNext(); err != nil {
		t.Fatal(err)
	} else if !hasNext {
		t.Fatalf("Should have a container ready to pop")
	}

	parser.ParseF = func(b []byte) (Job, error) {
		if bytes.Equal(b, []byte{0}) {
			return job0, nil
		}
		if bytes.Equal(b, []byte{1}) {
			return job1, nil
		}
		t.Fatalf("Unknown job")
		return nil, errors.New("Unknown job")
	}

	returnedBlockable, err := jobs.Pop()
	if err != nil {
		t.Fatal(err)
	}

	if returnedBlockable != job0 {
		t.Fatalf("Returned wrong job")
	}

	if err := jobs.Execute(job0); err != nil {
		t.Fatal(err)
	}

	if hasNext, err := jobs.HasNext(); err != nil {
		t.Fatal(err)
	} else if !hasNext {
		t.Fatalf("Should have a container ready to pop")
	}

	returnedBlockable, err = jobs.Pop()
	if err != nil {
		t.Fatal(err)
	}

	if returnedBlockable != job1 {
		t.Fatalf("Returned wrong job")
	}

	if hasNext, err := jobs.HasNext(); err != nil {
		t.Fatal(err)
	} else if hasNext {
		t.Fatalf("Shouldn't have a container ready to pop")
	}
}

func TestPendingJobs(t *testing.T) {
	parser := &TestParser{T: t}
	db := memdb.New()

	jobs, err := New(db)
	if err != nil {
		t.Fatal(err)
	}

	jobs.SetParser(parser)

	id0 := ids.Empty.Prefix(0)
	id1 := ids.Empty.Prefix(1)

	jobs.AddPendingID(id0)
	jobs.AddPendingID(id1)

	if err := jobs.Commit(); err != nil {
		t.Fatal(err)
	}

	if pending := jobs.PendingJobs(); pending != 2 {
		t.Fatalf("Expected number of pending jobs to be 2, but was %d", pending)
	}

	pendingSet := ids.Set{}
	pendingSet.Add(jobs.Pending()...)

	if !pendingSet.Contains(id0) {
		t.Fatal("Expected pending to contain id0")
	}

	if !pendingSet.Contains(id1) {
		t.Fatal("Expected pending to contain id1")
	}

	jobs.RemovePendingID(id1)

	if err := jobs.Commit(); err != nil {
		t.Fatal(err)
	}

	newJobs, err := New(db)
	if err != nil {
		t.Fatal(err)
	}

	newJobs.SetParser(parser)

	pendingSet.Clear()
	pendingSet.Add(newJobs.Pending()...)
	if !pendingSet.Contains(id0) {
		t.Fatal("Expected pending to contain id0")
	}
}
