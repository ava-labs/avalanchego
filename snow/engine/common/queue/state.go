// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"fmt"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	dependentsCacheSize = 1024
	jobsCacheSize       = 4096
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
	jobsCache      cache.Cacher
	jobs           database.Database
	// Should be prefixed with the jobID that we are attempting to find the
	// dependencies of. This prefixdb.Database should then be wrapped in a
	// linkeddb.LinkedDB to read the dependencies.
	dependencies database.Database
	// This is a cache that tracks LinkedDB iterators that have recently been
	// made.
	dependentsCache cache.Cacher
	missingJobIDs   linkeddb.LinkedDB
}

func newState(
	db database.Database,
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) (*state, error) {
	jobsCacheMetricsNamespace := fmt.Sprintf("%s_jobs_cache", metricsNamespace)
	jobsCache, err := metercacher.New(jobsCacheMetricsNamespace, metricsRegisterer, &cache.LRU{Size: jobsCacheSize})
	if err != nil {
		return nil, fmt.Errorf("couldn't create metered cache: %s", err)
	}
	return &state{
		runnableJobIDs:  linkeddb.NewDefault(prefixdb.New(runnableJobIDsKey, db)),
		jobsCache:       jobsCache,
		jobs:            prefixdb.New(jobsKey, db),
		dependencies:    prefixdb.New(dependenciesKey, db),
		dependentsCache: &cache.LRU{Size: dependentsCacheSize},
		missingJobIDs:   linkeddb.NewDefault(prefixdb.New(missingJobIDsKey, db)),
	}, nil
}

// AddRunnableJob adds [jobID] to the runnable queue
func (ps *state) AddRunnableJob(jobID ids.ID) error {
	return ps.runnableJobIDs.Put(jobID[:], nil)
}

// HasRunnableJob returns if there is a job that can be run on the queue
func (ps *state) HasRunnableJob() (bool, error) {
	isEmpty, err := ps.runnableJobIDs.IsEmpty()
	return !isEmpty, err
}

// RemoveRunnableJob fetches and deletes the next job from the runnable queue
func (ps *state) RemoveRunnableJob() (Job, error) {
	jobIDBytes, err := ps.runnableJobIDs.HeadKey()
	if err != nil {
		return nil, err
	}
	if err := ps.runnableJobIDs.Delete(jobIDBytes); err != nil {
		return nil, err
	}

	jobID, err := ids.ToID(jobIDBytes)
	if err != nil {
		return nil, fmt.Errorf("couldn't convert job ID bytes to job ID: %s", err)
	}
	jobIntf, exists := ps.jobsCache.Get(jobID)
	if exists {
		ps.jobsCache.Evict(jobID)
		return jobIntf.(Job), ps.jobs.Delete(jobIDBytes)
	}

	jobBytes, err := ps.jobs.Get(jobIDBytes)
	if err != nil {
		return nil, err
	}
	job, err := ps.parser.Parse(jobBytes)
	if err != nil {
		return nil, err
	}
	ps.jobsCache.Evict(jobID)
	return job, ps.jobs.Delete(jobIDBytes)
}

// PutJob adds the job to the queue
func (ps *state) PutJob(job Job) error {
	id := job.ID()
	ps.jobsCache.Put(id, job)
	return ps.jobs.Put(id[:], job.Bytes())
}

// HasJob returns true if the job [id] is in the queue
func (ps *state) HasJob(id ids.ID) (bool, error) {
	if _, exists := ps.jobsCache.Get(id); exists {
		return true, nil
	}
	return ps.jobs.Has(id[:])
}

// GetJob returns the job [id]
func (ps *state) GetJob(id ids.ID) (Job, error) {
	if job, exists := ps.jobsCache.Get(id); exists {
		return job.(Job), nil
	}
	jobBytes, err := ps.jobs.Get(id[:])
	if err != nil {
		return nil, err
	}
	return ps.parser.Parse(jobBytes)
}

// AddBlocking adds [dependent] as blocking on [dependency] being completed
func (ps *state) AddDependency(dependency, dependent ids.ID) error {
	dependentsDB := ps.getDependents(dependency)
	return dependentsDB.Put(dependent[:], nil)
}

// Blocking returns the set of IDs that are blocking on the completion of
// [dependency] and removes them from the database.
func (ps *state) RemoveDependencies(dependency ids.ID) ([]ids.ID, error) {
	dependentsDB := ps.getDependents(dependency)
	iterator := dependentsDB.NewIterator()
	defer iterator.Release()

	dependents := []ids.ID(nil)
	for iterator.Next() {
		dependentKey := iterator.Key()
		if err := dependentsDB.Delete(dependentKey); err != nil {
			return nil, err
		}
		dependent, err := ids.ToID(dependentKey)
		if err != nil {
			return nil, err
		}
		dependents = append(dependents, dependent)
	}
	return dependents, iterator.Error()
}

func (ps *state) AddMissingJobIDs(missingIDs ids.Set) error {
	for missingID := range missingIDs {
		missingID := missingID
		if err := ps.missingJobIDs.Put(missingID[:], nil); err != nil {
			return err
		}
	}
	return nil
}

func (ps *state) RemoveMissingJobIDs(missingIDs ids.Set) error {
	for missingID := range missingIDs {
		missingID := missingID
		if err := ps.missingJobIDs.Delete(missingID[:]); err != nil {
			return err
		}
	}
	return nil
}

func (ps *state) MissingJobIDs() ([]ids.ID, error) {
	iterator := ps.missingJobIDs.NewIterator()
	defer iterator.Release()

	missingIDs := []ids.ID(nil)
	for iterator.Next() {
		missingID, err := ids.ToID(iterator.Key())
		if err != nil {
			return nil, err
		}
		missingIDs = append(missingIDs, missingID)
	}
	return missingIDs, nil
}

func (ps *state) getDependents(dependency ids.ID) linkeddb.LinkedDB {
	if dependentsDBIntf, ok := ps.dependentsCache.Get(dependency); ok {
		return dependentsDBIntf.(linkeddb.LinkedDB)
	}
	dependencyDB := prefixdb.New(dependency[:], ps.dependencies)
	dependentsDB := linkeddb.NewDefault(dependencyDB)
	ps.dependentsCache.Put(dependency, dependentsDB)
	return dependentsDB
}
