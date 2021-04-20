// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

// TestJob is a test Job
type TestJob struct {
	T *testing.T

	CantID,
	CantMissingDependencies,
	CantExecute,
	CantBytes bool

	IDF                  func() ids.ID
	MissingDependenciesF func() (ids.Set, error)
	ExecuteF             func() error
	BytesF               func() []byte
}

func (j *TestJob) Default(cant bool) {
	j.CantID = cant
	j.CantMissingDependencies = cant
	j.CantExecute = cant
	j.CantBytes = cant
}

func (j *TestJob) ID() ids.ID {
	if j.IDF != nil {
		return j.IDF()
	}
	if j.CantID && j.T != nil {
		j.T.Fatalf("Unexpectedly called ID")
	}
	return ids.ID{}
}

func (j *TestJob) MissingDependencies() (ids.Set, error) {
	if j.MissingDependenciesF != nil {
		return j.MissingDependenciesF()
	}
	if j.CantMissingDependencies && j.T != nil {
		j.T.Fatalf("Unexpectedly called MissingDependencies")
	}
	return ids.Set{}, nil
}

func (j *TestJob) Execute() error {
	if j.ExecuteF != nil {
		return j.ExecuteF()
	}
	if j.CantExecute && j.T != nil {
		j.T.Fatalf("Unexpectedly called Execute")
	}
	return errors.New("unexpectedly called Execute")
}

func (j *TestJob) Bytes() []byte {
	if j.BytesF != nil {
		return j.BytesF()
	}
	if j.CantBytes && j.T != nil {
		j.T.Fatalf("Unexpectedly called Bytes")
	}
	return nil
}
