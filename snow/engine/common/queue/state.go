// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
)

var (
	runnableJobIDsKey = []byte("runnable")
	jobsKey           = []byte("jobs")
	dependenciesKey   = []byte("dependencies")
	missingJobIDsKey  = []byte("missing job IDs")
)

type state struct {
	parser         Parser
	runnableJobIDs linkeddb.LinkedDB
	jobs           database.Database
	// Should be prefixed with the jobID that we are attempting to find the
	// dependencies of. This prefixdb.Database should then be wrapped in a
	// linkeddb.LinkedDB to read the dependencies.
	dependencies  database.Database
	missingJobIDs linkeddb.LinkedDB
}

func newState(db database.Database) *state {
	return &state{
		runnableJobIDs: linkeddb.NewDefault(prefixdb.New(runnableJobIDsKey, db)),
		jobs:           prefixdb.New(jobsKey, db),
		dependencies:   prefixdb.New(dependenciesKey, db),
		missingJobIDs:  linkeddb.NewDefault(prefixdb.New(missingJobIDsKey, db)),
	}
}

// AddRunnableJob adds [jobID] to the runnable queue
func (ps *state) AddRunnableJob(jobID ids.ID) error {
	return ps.runnableJobIDs.Put(jobID[:], nil)
}

// HasRunnableJob returns if there is a job that can be run on the queue.
func (ps *state) HasRunnableJob() (bool, error) {
	isEmpty, err := ps.runnableJobIDs.IsEmpty()
	return !isEmpty, err
}

// RemoveRunnableJob fetches and deletes the next job from the runnable queue
func (ps *state) RemoveRunnableJob() (Job, error) {
	key, err := ps.runnableJobIDs.HeadKey()
	if err != nil {
		return nil, err
	}
	if err := ps.runnableJobIDs.Delete(key); err != nil {
		return nil, err
	}

	jobBytes, err := ps.jobs.Get(key)
	if err != nil {
		return nil, err
	}
	job, err := ps.parser.Parse(jobBytes)
	if err != nil {
		return nil, err
	}
	return job, ps.jobs.Delete(key)
}

// PutJob adds the job to the queue
func (ps *state) PutJob(job Job) error {
	id := job.ID()
	return ps.jobs.Put(id[:], job.Bytes())
}

// HasJob returns true if the job [id] is in the queue
func (ps *state) HasJob(id ids.ID) (bool, error) {
	return ps.jobs.Has(id[:])
}

// AddBlocking adds [dependent] as blocking on [dependency] being completed
func (ps *state) AddDependency(dependency, dependent ids.ID) error {
	dependencyDB := prefixdb.New(dependency[:], ps.dependencies)
	dependentsDB := linkeddb.NewDefault(dependencyDB)
	if err := dependentsDB.Put(dependent[:], nil); err != nil {
		return err
	}
	return dependencyDB.Close()
}

// Blocking returns the set of IDs that are blocking on the completion of
// [dependency] and removes them from the database.
func (ps *state) RemoveDependencies(dependency ids.ID) ([]ids.ID, error) {
	dependencyDB := prefixdb.New(dependency[:], ps.dependencies)
	dependentsDB := linkeddb.NewDefault(dependencyDB)

	iterator := dependentsDB.NewIterator()
	defer iterator.Release()

	dependents := []ids.ID(nil)
	for iterator.Next() {
		dependentKey := iterator.Key()
		if err := dependentsDB.Delete(dependentKey); err != nil {
			_ = dependencyDB.Close()
			return nil, err
		}
		dependent, err := ids.ToID(dependentKey)
		if err != nil {
			_ = dependencyDB.Close()
			return nil, err
		}
		dependents = append(dependents, dependent)
	}

	if err := dependencyDB.Close(); err != nil {
		return nil, err
	}
	return dependents, iterator.Error()
}

func (ps *state) AddPending(db database.Database, pendingIDs ids.Set) error {
	return ps.state.AddIDs(db, pendingPrefix, pendingIDs)
}

func (ps *state) RemovePending(db database.Database, pendingIDs ids.Set) error {
	return ps.state.RemoveIDs(db, pendingPrefix, pendingIDs)
}

func (ps *state) Pending(db database.Database) ([]ids.ID, error) {
	return ps.state.IDs(db, pendingPrefix)
}
