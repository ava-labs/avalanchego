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
	jobsCacheSize       = 2048
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
	cachingEnabled bool
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
		cachingEnabled:  true,
		jobsCache:       jobsCache,
		jobs:            prefixdb.New(jobsKey, db),
		dependencies:    prefixdb.New(dependenciesKey, db),
		dependentsCache: &cache.LRU{Size: dependentsCacheSize},
		missingJobIDs:   linkeddb.NewDefault(prefixdb.New(missingJobIDsKey, db)),
	}, nil
}

// AddRunnableJob adds [jobID] to the runnable queue
func (s *state) AddRunnableJob(jobID ids.ID) error {
	return s.runnableJobIDs.Put(jobID[:], nil)
}

// HasRunnableJob returns if there is a job that can be run on the queue
func (s *state) HasRunnableJob() (bool, error) {
	isEmpty, err := s.runnableJobIDs.IsEmpty()
	return !isEmpty, err
}

// RemoveRunnableJob fetches and deletes the next job from the runnable queue
func (s *state) RemoveRunnableJob() (Job, error) {
	jobIDBytes, err := s.runnableJobIDs.HeadKey()
	if err != nil {
		return nil, err
	}
	if err := s.runnableJobIDs.Delete(jobIDBytes); err != nil {
		return nil, err
	}

	jobID, err := ids.ToID(jobIDBytes)
	if err != nil {
		return nil, fmt.Errorf("couldn't convert job ID bytes to job ID: %s", err)
	}
	if s.cachingEnabled {
		jobIntf, exists := s.jobsCache.Get(jobID)
		if exists {
			s.jobsCache.Evict(jobID)
			return jobIntf.(Job), s.jobs.Delete(jobIDBytes)
		}
	}

	jobBytes, err := s.jobs.Get(jobIDBytes)
	if err != nil {
		return nil, err
	}
	job, err := s.parser.Parse(jobBytes)
	if err != nil {
		return nil, err
	}
	return job, s.jobs.Delete(jobIDBytes)
}

// PutJob adds the job to the queue
func (s *state) PutJob(job Job) error {
	id := job.ID()
	if s.cachingEnabled {
		s.jobsCache.Put(id, job)
	}
	return s.jobs.Put(id[:], job.Bytes())
}

// HasJob returns true if the job [id] is in the queue
func (s *state) HasJob(id ids.ID) (bool, error) {
	if s.cachingEnabled {
		if _, exists := s.jobsCache.Get(id); exists {
			return true, nil
		}
	}
	return s.jobs.Has(id[:])
}

// GetJob returns the job [id]
func (s *state) GetJob(id ids.ID) (Job, error) {
	if s.cachingEnabled {
		if job, exists := s.jobsCache.Get(id); exists {
			return job.(Job), nil
		}
	}
	jobBytes, err := s.jobs.Get(id[:])
	if err != nil {
		return nil, err
	}
	job, err := s.parser.Parse(jobBytes)
	if err == nil && s.cachingEnabled {
		s.jobsCache.Put(id, job)
	}
	return job, err
}

// AddBlocking adds [dependent] as blocking on [dependency] being completed
func (s *state) AddDependency(dependency, dependent ids.ID) error {
	dependentsDB := s.getDependentsDB(dependency)
	return dependentsDB.Put(dependent[:], nil)
}

// Blocking returns the set of IDs that are blocking on the completion of
// [dependency] and removes them from the database.
func (s *state) RemoveDependencies(dependency ids.ID) ([]ids.ID, error) {
	dependentsDB := s.getDependentsDB(dependency)
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

func (s *state) DisableCaching() {
	s.dependentsCache.Flush()
	s.jobsCache.Flush()
	s.cachingEnabled = false
}

func (s *state) AddMissingJobIDs(missingIDs ids.Set) error {
	for missingID := range missingIDs {
		missingID := missingID
		if err := s.missingJobIDs.Put(missingID[:], nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *state) RemoveMissingJobIDs(missingIDs ids.Set) error {
	for missingID := range missingIDs {
		missingID := missingID
		if err := s.missingJobIDs.Delete(missingID[:]); err != nil {
			return err
		}
	}
	return nil
}

func (s *state) MissingJobIDs() ([]ids.ID, error) {
	iterator := s.missingJobIDs.NewIterator()
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

func (s *state) getDependentsDB(dependency ids.ID) linkeddb.LinkedDB {
	if s.cachingEnabled {
		if dependentsDBIntf, ok := s.dependentsCache.Get(dependency); ok {
			return dependentsDBIntf.(linkeddb.LinkedDB)
		}
	}
	dependencyDB := prefixdb.New(dependency[:], s.dependencies)
	dependentsDB := linkeddb.NewDefault(dependencyDB)
	if s.cachingEnabled {
		s.dependentsCache.Put(dependency, dependentsDB)
	}
	return dependentsDB
}
