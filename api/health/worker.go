// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils"
)

var errDuplicateCheck = errors.New("duplicated check")

type worker struct {
	metrics    *metrics
	checksLock sync.RWMutex
	checks     map[string]Checker

	resultsLock sync.RWMutex
	results     map[string]Result

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
	}, err
}

func (w *worker) RegisterCheck(name string, checker Checker) error {
	w.checksLock.Lock()
	defer w.checksLock.Unlock()

	if _, ok := w.checks[name]; ok {
		return fmt.Errorf("%w: %q", errDuplicateCheck, name)
	}

	w.resultsLock.Lock()
	defer w.resultsLock.Unlock()

	w.checks[name] = checker
	w.results[name] = notYetRunResult

	// Whenever a new check is added - it is failing
	w.metrics.failingChecks.Inc()
	return nil
}

func (w *worker) RegisterMonotonicCheck(name string, checker Checker) error {
	var result utils.AtomicInterface
	return w.RegisterCheck(name, CheckerFunc(func() (interface{}, error) {
		details := result.GetValue()
		if details != nil {
			return details, nil
		}

		details, err := checker.HealthCheck()
		if err == nil {
			result.SetValue(details)
		}
		return details, err
	}))
}

func (w *worker) Results() (map[string]Result, bool) {
	w.resultsLock.RLock()
	defer w.resultsLock.RUnlock()

	results := make(map[string]Result, len(w.results))
	healthy := true
	for name, result := range w.results {
		results[name] = result
		healthy = healthy && result.Error == nil
	}
	return results, healthy
}

func (w *worker) Start(freq time.Duration) {
	w.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(freq)
			defer ticker.Stop()

			newResults := w.runChecks()

			w.resultsLock.Lock()
			w.results = newResults
			w.resultsLock.Unlock()

			for {
				select {
				case <-ticker.C:
					newResults := w.runChecks()

					w.resultsLock.Lock()
					w.results = newResults
					w.resultsLock.Unlock()
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

func (w *worker) runChecks() map[string]Result {
	w.checksLock.RLock()
	defer w.checksLock.RUnlock()

	w.resultsLock.RLock()
	defer w.resultsLock.RUnlock()

	var (
		resultsLock sync.Mutex
		results     = make(map[string]Result, len(w.checks))
		wg          sync.WaitGroup
	)
	wg.Add(len(w.checks))
	for name, check := range w.checks {
		go func(name string, check Checker) {
			defer wg.Done()

			start := time.Now()
			details, err := check.HealthCheck()
			end := time.Now()

			result := Result{
				Details:   details,
				Timestamp: end,
				Duration:  end.Sub(start),
			}

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

			resultsLock.Lock()
			results[name] = result
			resultsLock.Unlock()
		}(name, check)
	}
	wg.Wait()
	return results
}
