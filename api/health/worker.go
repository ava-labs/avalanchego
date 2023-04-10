// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
)

var errDuplicateCheck = errors.New("duplicated check")

type worker struct {
	metrics    *metrics
	checksLock sync.RWMutex
	checks     map[string]Checker

	resultsLock sync.RWMutex
	results     map[string]Result
	tags        map[string]set.Set[string] // tag -> set of check names

	startOnce sync.Once
	closeOnce sync.Once
	closer    chan struct{}
}

func newWorker(namespace string, registerer prometheus.Registerer) (*worker, error) {
	metrics, err := newMetrics(namespace, registerer)
	return &worker{
		metrics: metrics,
		checks:  make(map[string]Checker),
		results: make(map[string]Result),
		closer:  make(chan struct{}),
		tags:    make(map[string]set.Set[string]),
	}, err
}

func (w *worker) RegisterCheck(name string, checker Checker, tags ...string) error {
	w.checksLock.Lock()
	defer w.checksLock.Unlock()

	if _, ok := w.checks[name]; ok {
		return fmt.Errorf("%w: %q", errDuplicateCheck, name)
	}

	w.resultsLock.Lock()
	defer w.resultsLock.Unlock()

	w.checks[name] = checker
	w.results[name] = notYetRunResult

	// Add the check to the tag
	for _, tag := range tags {
		names := w.tags[tag]
		names.Add(name)
		w.tags[tag] = names
	}

	// Whenever a new check is added - it is failing
	w.metrics.failingChecks.Inc()
	return nil
}

func (w *worker) RegisterMonotonicCheck(name string, checker Checker, tags ...string) error {
	var result utils.Atomic[any]
	return w.RegisterCheck(name, CheckerFunc(func(ctx context.Context) (any, error) {
		details := result.Get()
		if details != nil {
			return details, nil
		}

		details, err := checker.HealthCheck(ctx)
		if err == nil {
			result.Set(details)
		}
		return details, err
	}), tags...)
}

func (w *worker) Results(tags ...string) (map[string]Result, bool) {
	w.resultsLock.RLock()
	defer w.resultsLock.RUnlock()

	results := make(map[string]Result, len(w.results))
	healthy := true

	// if tags are specified, iterate through registered check names in the tag
	if len(tags) > 0 {
		names := set.Set[string]{}
		// prepare tagSet for global tag
		tagSet := set.NewSet[string](len(tags) + 1)
		tagSet.Add(tags...)
		// we always want to include the global tag
		tagSet.Add(GlobalTag)
		for tag := range tagSet {
			if set, ok := w.tags[tag]; ok {
				names.Union(set)
			}
		}
		for name := range names {
			if result, ok := w.results[name]; ok {
				results[name] = result
				healthy = healthy && result.Error == nil
			}
		}
	} else { // if tags are not specified, iterate through all registered check names
		for name, result := range w.results {
			results[name] = result
			healthy = healthy && result.Error == nil
		}
	}

	return results, healthy
}

func (w *worker) Start(ctx context.Context, freq time.Duration) {
	w.startOnce.Do(func() {
		detachedCtx := utils.Detach(ctx)
		go func() {
			ticker := time.NewTicker(freq)
			defer ticker.Stop()

			w.runChecks(detachedCtx)
			for {
				select {
				case <-ticker.C:
					w.runChecks(detachedCtx)
				case <-w.closer:
					return
				}
			}
		}()
	})
}

func (w *worker) Stop() {
	w.closeOnce.Do(func() {
		close(w.closer)
	})
}

func (w *worker) runChecks(ctx context.Context) {
	w.checksLock.RLock()
	// Copy the [w.checks] map to collect the checks that we will be running
	// during this iteration. If [w.checks] is modified during this iteration of
	// [runChecks], then the added check will not be run until the next
	// iteration.
	checks := maps.Clone(w.checks)
	w.checksLock.RUnlock()

	var wg sync.WaitGroup
	wg.Add(len(checks))
	for name, check := range checks {
		go w.runCheck(ctx, &wg, name, check)
	}
	wg.Wait()
}

func (w *worker) runCheck(ctx context.Context, wg *sync.WaitGroup, name string, check Checker) {
	defer wg.Done()

	start := time.Now()

	// To avoid any deadlocks when [RegisterCheck] is called with a lock
	// that is grabbed by [check.HealthCheck], we ensure that no locks
	// are held when [check.HealthCheck] is called.
	details, err := check.HealthCheck(ctx)
	end := time.Now()

	result := Result{
		Details:   details,
		Timestamp: end,
		Duration:  end.Sub(start),
	}

	w.resultsLock.Lock()
	defer w.resultsLock.Unlock()
	prevResult := w.results[name]
	if err != nil {
		errString := err.Error()
		result.Error = &errString

		result.ContiguousFailures = prevResult.ContiguousFailures + 1
		if prevResult.ContiguousFailures > 0 {
			result.TimeOfFirstFailure = prevResult.TimeOfFirstFailure
		} else {
			result.TimeOfFirstFailure = &end
		}

		if prevResult.Error == nil {
			w.metrics.failingChecks.Inc()
		}
	} else if prevResult.Error != nil {
		w.metrics.failingChecks.Dec()
	}
	w.results[name] = result
}
