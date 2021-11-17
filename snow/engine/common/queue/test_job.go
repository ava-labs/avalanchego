// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	errExecute                = errors.New("unexpectedly called Execute")
	errHasMissingDependencies = errors.New("unexpectedly called HasMissingDependencies")
)

// TestJob is a test Job
type TestJob struct {
	T *testing.T

	CantID,
	CantMissingDependencies,
	CantExecute,
	CantBytes,
	CantHasMissingDependencies bool

	IDF                     func() ids.ID
	MissingDependenciesF    func() (ids.Set, error)
	ExecuteF                func() error
	BytesF                  func() []byte
	HasMissingDependenciesF func() (bool, error)
}

func (j *TestJob) Default(cant bool) {
	j.CantID = cant
	j.CantMissingDependencies = cant
	j.CantExecute = cant
	j.CantBytes = cant
	j.CantHasMissingDependencies = cant
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
		j.T.Fatal(errExecute)
	}
	return errExecute
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

func (j *TestJob) HasMissingDependencies() (bool, error) {
	if j.HasMissingDependenciesF != nil {
		return j.HasMissingDependenciesF()
	}
	if j.CantHasMissingDependencies && j.T != nil {
		j.T.Fatal(errHasMissingDependencies)
	}
	return false, errHasMissingDependencies
}
