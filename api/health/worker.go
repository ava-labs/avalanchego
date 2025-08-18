// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	allTags = []string{AllTag}

	errRestrictedTag  = errors.New("restricted tag")
	errDuplicateCheck = errors.New("duplicate check")
)

type worker struct {
	log           logging.Logger
	name          string
	failingChecks *prometheus.GaugeVec
	checksLock    sync.RWMutex
	checks        map[string]*taggedChecker

	resultsLock                 sync.RWMutex
	results                     map[string]Result
	numFailingApplicationChecks int
	tags                        map[string]set.Set[string] // tag -> set of check names

	startOnce sync.Once
	closeOnce sync.Once
	wg        sync.WaitGroup
	closer    chan struct{}
}

type taggedChecker struct {
	checker            Checker
	isApplicationCheck bool
	tags               []string
}

func newWorker(
	log logging.Logger,
	name string,
	failingChecks *prometheus.GaugeVec,
) *worker {
	// Initialize the number of failing checks to 0 for all checks
	for _, tag := range []string{AllTag, ApplicationTag} {
		failingChecks.With(prometheus.Labels{
			CheckLabel: name,
			TagLabel:   tag,
		}).Set(0)
	}
	return &worker{
		log:           log,
		name:          name,
		failingChecks: failingChecks,
		checks:        make(map[string]*taggedChecker),
		results:       make(map[string]Result),
		closer:        make(chan struct{}),
		tags:          make(map[string]set.Set[string]),
	}
}

func (w *worker) RegisterCheck(name string, check Checker, tags ...string) error {
	// We ensure [AllTag] isn't contained in [tags] to prevent metrics from
	// double counting.
	if slices.Contains(tags, AllTag) {
		return fmt.Errorf("%w: %q", errRestrictedTag, AllTag)
	}

	w.checksLock.Lock()
	defer w.checksLock.Unlock()

	if _, ok := w.checks[name]; ok {
		return fmt.Errorf("%w: %q", errDuplicateCheck, name)
	}

	w.resultsLock.Lock()
	defer w.resultsLock.Unlock()

	// Add the check to each tag
	for _, tag := range tags {
		names := w.tags[tag]
		names.Add(name)
		w.tags[tag] = names
	}
	// Add the special AllTag descriptor
	names := w.tags[AllTag]
	names.Add(name)
	w.tags[AllTag] = names

	applicationChecks := w.tags[ApplicationTag]
	tc := &taggedChecker{
		checker:            check,
		isApplicationCheck: applicationChecks.Contains(name),
		tags:               tags,
	}
	w.checks[name] = tc
	w.results[name] = notYetRunResult

	// Whenever a new check is added - it is failing
	w.log.Info("registered new check and initialized its state to failing",
		zap.String("name", w.name),
		zap.String("name", name),
		zap.Strings("tags", tags),
	)

	// If this is a new application-wide check, then all of the registered tags
	// now have one additional failing check.
	w.updateMetrics(tc, false /*=healthy*/, true /*=register*/)
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

	// if no tags are specified, return all checks
	if len(tags) == 0 {
		tags = allTags
	}

	names := set.Set[string]{}
	tagSet := set.Of(tags...)
	tagSet.Add(ApplicationTag) // we always want to include the application tag
	for tag := range tagSet {
		if set, ok := w.tags[tag]; ok {
			names.Union(set)
		}
	}

	results := make(map[string]Result, names.Len())
	healthy := true
	for name := range names {
		if result, ok := w.results[name]; ok {
			results[name] = result
			healthy = healthy && result.Error == nil
		}
	}
	return results, healthy
}

func (w *worker) Start(ctx context.Context, freq time.Duration) {
	w.startOnce.Do(func() {
		detachedCtx := context.WithoutCancel(ctx)
		w.wg.Add(1)
		go func() {
			ticker := time.NewTicker(freq)
			defer func() {
				ticker.Stop()
				w.wg.Done()
			}()

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
		w.wg.Wait()
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

func (w *worker) runCheck(ctx context.Context, wg *sync.WaitGroup, name string, check *taggedChecker) {
	defer wg.Done()

	start := time.Now()

	// To avoid any deadlocks when [RegisterCheck] is called with a lock
	// that is grabbed by [check.HealthCheck], we ensure that no locks
	// are held when [check.HealthCheck] is called.
	details, err := check.checker.HealthCheck(ctx)
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
			w.log.Warn("check started failing",
				zap.String("name", w.name),
				zap.String("name", name),
				zap.Strings("tags", check.tags),
				zap.Error(err),
			)
			w.updateMetrics(check, false /*=healthy*/, false /*=register*/)
		}
	} else if prevResult.Error != nil {
		w.log.Info("check started passing",
			zap.String("name", w.name),
			zap.String("name", name),
			zap.Strings("tags", check.tags),
		)
		w.updateMetrics(check, true /*=healthy*/, false /*=register*/)
	}
	w.results[name] = result
}

// updateMetrics updates the metrics for the given check. If [healthy] is true,
// then the check is considered healthy and the metrics are decremented.
// Otherwise, the check is considered unhealthy and the metrics are incremented.
// [register] must be true only if this is the first time the check is being
// registered.
func (w *worker) updateMetrics(tc *taggedChecker, healthy bool, register bool) {
	if tc.isApplicationCheck {
		// Note: [w.tags] will include AllTag.
		for tag := range w.tags {
			gauge := w.failingChecks.With(prometheus.Labels{
				CheckLabel: w.name,
				TagLabel:   tag,
			})
			if healthy {
				gauge.Dec()
			} else {
				gauge.Inc()
			}
		}
		if healthy {
			w.numFailingApplicationChecks--
		} else {
			w.numFailingApplicationChecks++
		}
	} else {
		for _, tag := range tc.tags {
			gauge := w.failingChecks.With(prometheus.Labels{
				CheckLabel: w.name,
				TagLabel:   tag,
			})
			if healthy {
				gauge.Dec()
			} else {
				gauge.Inc()
				// If this is the first time this tag was registered, we also need to
				// account for the currently failing application-wide checks.
				if register && w.tags[tag].Len() == 1 {
					gauge.Add(float64(w.numFailingApplicationChecks))
				}
			}
		}
		gauge := w.failingChecks.With(prometheus.Labels{
			CheckLabel: w.name,
			TagLabel:   AllTag,
		})
		if healthy {
			gauge.Dec()
		} else {
			gauge.Inc()
		}
	}
}
