// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"bytes"
	"testing"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
)

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
		MissingDependenciesF: func() ids.Set { return ids.Set{} },
		ExecuteF:             func() {},
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
		MissingDependenciesF: func() ids.Set { return ids.Set{} },
		ExecuteF:             func() { *executed0 = true },
		BytesF:               func() []byte { return []byte{0} },
	}

	id1 := ids.Empty.Prefix(0)
	executed1 := new(bool)
	job1 := &TestJob{
		T: t,

		IDF:                  func() ids.ID { return id1 },
		MissingDependenciesF: func() ids.Set { return ids.Set{id0.Key(): true} },
		ExecuteF:             func() { *executed1 = true },
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

	job1.MissingDependenciesF = func() ids.Set { return ids.Set{} }
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

func TestDuplicatedPush(t *testing.T) {
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
		MissingDependenciesF: func() ids.Set { return ids.Set{} },
		ExecuteF:             func() {},
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
