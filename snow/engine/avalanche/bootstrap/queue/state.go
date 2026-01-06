// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package queue

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	dependentsCacheSize = 1024
	jobsCacheSize       = 2048
)

var (
	runnableJobIDsPrefix = []byte("runnable")
	jobsPrefix           = []byte("jobs")
	dependenciesPrefix   = []byte("dependencies")
	missingJobIDsPrefix  = []byte("missing job IDs")
	metadataPrefix       = []byte("metadata")
	numJobsKey           = []byte("numJobs")
)

type state struct {
	parser         Parser
	runnableJobIDs linkeddb.LinkedDB
	cachingEnabled bool
	jobsCache      cache.Cacher[ids.ID, Job]
	jobsDB         database.Database
	// Should be prefixed with the jobID that we are attempting to find the
	// dependencies of. This prefixdb.Database should then be wrapped in a
	// linkeddb.LinkedDB to read the dependencies.
	dependenciesDB database.Database
	// This is a cache that tracks LinkedDB iterators that have recently been
	// made.
	dependentsCache cache.Cacher[ids.ID, linkeddb.LinkedDB]
	missingJobIDs   linkeddb.LinkedDB
	// This tracks the summary values of this state. Currently, this only
	// contains the last known checkpoint of how many jobs are currently in the
	// queue to execute.
	metadataDB database.Database
	// This caches the number of jobs that are currently in the queue to
	// execute.
	numJobs uint64
}

func newState(
	db database.Database,
	metricsNamespace string,
	metricsRegisterer prometheus.Registerer,
) (*state, error) {
	jobsCacheMetricsNamespace := metric.AppendNamespace(metricsNamespace, "jobs_cache")
	jobsCache, err := metercacher.New[ids.ID, Job](
		jobsCacheMetricsNamespace,
		metricsRegisterer,
		lru.NewCache[ids.ID, Job](jobsCacheSize),
	)
	if err != nil {
		return nil, fmt.Errorf("couldn't create metered cache: %w", err)
	}

	metadataDB := prefixdb.New(metadataPrefix, db)
	jobs := prefixdb.New(jobsPrefix, db)
	numJobs, err := getNumJobs(metadataDB, jobs)
	if err != nil {
		return nil, fmt.Errorf("couldn't initialize pending jobs: %w", err)
	}
	return &state{
		runnableJobIDs:  linkeddb.NewDefault(prefixdb.New(runnableJobIDsPrefix, db)),
		cachingEnabled:  true,
		jobsCache:       jobsCache,
		jobsDB:          jobs,
		dependenciesDB:  prefixdb.New(dependenciesPrefix, db),
		dependentsCache: lru.NewCache[ids.ID, linkeddb.LinkedDB](dependentsCacheSize),
		missingJobIDs:   linkeddb.NewDefault(prefixdb.New(missingJobIDsPrefix, db)),
		metadataDB:      metadataDB,
		numJobs:         numJobs,
	}, nil
}

func getNumJobs(d database.Database, jobs database.Iteratee) (uint64, error) {
	numJobs, err := database.GetUInt64(d, numJobsKey)
	if err == database.ErrNotFound {
		// If we don't have a checkpoint, we need to initialize it.
		count, err := database.Count(jobs)
		return uint64(count), err
	}
	return numJobs, err
}

func (s *state) Clear() error {
	var (
		runJobsIter  = s.runnableJobIDs.NewIterator()
		jobsIter     = s.jobsDB.NewIterator()
		depsIter     = s.dependenciesDB.NewIterator()
		missJobsIter = s.missingJobIDs.NewIterator()
	)
	defer func() {
		runJobsIter.Release()
		jobsIter.Release()
		depsIter.Release()
		missJobsIter.Release()
	}()

	// clear runnableJobIDs
	for runJobsIter.Next() {
		if err := s.runnableJobIDs.Delete(runJobsIter.Key()); err != nil {
			return err
		}
	}

	// clear jobs
	s.jobsCache.Flush()
	for jobsIter.Next() {
		if err := s.jobsDB.Delete(jobsIter.Key()); err != nil {
			return err
		}
	}

	// clear dependencies
	s.dependentsCache.Flush()
	for depsIter.Next() {
		if err := s.dependenciesDB.Delete(depsIter.Key()); err != nil {
			return err
		}
	}

	// clear missing jobs IDs
	for missJobsIter.Next() {
		if err := s.missingJobIDs.Delete(missJobsIter.Key()); err != nil {
			return err
		}
	}

	// clear number of pending jobs
	s.numJobs = 0
	if err := database.PutUInt64(s.metadataDB, numJobsKey, s.numJobs); err != nil {
		return err
	}

	return errors.Join(
		runJobsIter.Error(),
		jobsIter.Error(),
		depsIter.Error(),
		missJobsIter.Error(),
	)
}

// AddRunnableJob adds [jobID] to the runnable queue
func (s *state) AddRunnableJob(jobID ids.ID) error {
	return s.runnableJobIDs.Put(jobID[:], nil)
}

// HasRunnableJob returns true if there is a job that can be run on the queue
func (s *state) HasRunnableJob() (bool, error) {
	isEmpty, err := s.runnableJobIDs.IsEmpty()
	return !isEmpty, err
}

// RemoveRunnableJob fetches and deletes the next job from the runnable queue
func (s *state) RemoveRunnableJob(ctx context.Context) (Job, error) {
	jobIDBytes, err := s.runnableJobIDs.HeadKey()
	if err != nil {
		return nil, err
	}
	if err := s.runnableJobIDs.Delete(jobIDBytes); err != nil {
		return nil, err
	}

	jobID, err := ids.ToID(jobIDBytes)
	if err != nil {
		return nil, fmt.Errorf("couldn't convert job ID bytes to job ID: %w", err)
	}
	job, err := s.GetJob(ctx, jobID)
	if err != nil {
		return nil, err
	}

	if err := s.jobsDB.Delete(jobIDBytes); err != nil {
		return job, err
	}

	// Guard rail to make sure we don't underflow.
	if s.numJobs == 0 {
		return job, nil
	}
	s.numJobs--

	return job, database.PutUInt64(s.metadataDB, numJobsKey, s.numJobs)
}

// PutJob adds the job to the queue
func (s *state) PutJob(job Job) error {
	id := job.ID()
	if s.cachingEnabled {
		s.jobsCache.Put(id, job)
	}

	if err := s.jobsDB.Put(id[:], job.Bytes()); err != nil {
		return err
	}

	s.numJobs++
	return database.PutUInt64(s.metadataDB, numJobsKey, s.numJobs)
}

// HasJob returns true if the job [id] is in the queue
func (s *state) HasJob(id ids.ID) (bool, error) {
	if s.cachingEnabled {
		if _, exists := s.jobsCache.Get(id); exists {
			return true, nil
		}
	}
	return s.jobsDB.Has(id[:])
}

// GetJob returns the job [id]
func (s *state) GetJob(ctx context.Context, id ids.ID) (Job, error) {
	if s.cachingEnabled {
		if job, exists := s.jobsCache.Get(id); exists {
			return job, nil
		}
	}
	jobBytes, err := s.jobsDB.Get(id[:])
	if err != nil {
		return nil, err
	}
	job, err := s.parser.Parse(ctx, jobBytes)
	if err == nil && s.cachingEnabled {
		s.jobsCache.Put(id, job)
	}
	return job, err
}

// AddDependency adds [dependent] as blocking on [dependency] being completed
func (s *state) AddDependency(dependency, dependent ids.ID) error {
	dependentsDB := s.getDependentsDB(dependency)
	return dependentsDB.Put(dependent[:], nil)
}

// RemoveDependencies removes the set of IDs that are blocking on the completion
// of [dependency] from the database and returns them.
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

func (s *state) AddMissingJobIDs(missingIDs set.Set[ids.ID]) error {
	for missingID := range missingIDs {
		if err := s.missingJobIDs.Put(missingID[:], nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *state) RemoveMissingJobIDs(missingIDs set.Set[ids.ID]) error {
	for missingID := range missingIDs {
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
	return missingIDs, iterator.Error()
}

func (s *state) getDependentsDB(dependency ids.ID) linkeddb.LinkedDB {
	if s.cachingEnabled {
		if dependentsDB, ok := s.dependentsCache.Get(dependency); ok {
			return dependentsDB
		}
	}
	dependencyDB := prefixdb.New(dependency[:], s.dependenciesDB)
	dependentsDB := linkeddb.NewDefault(dependencyDB)
	if s.cachingEnabled {
		s.dependentsCache.Put(dependency, dependentsDB)
	}
	return dependentsDB
}
