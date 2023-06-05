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

	"go.uber.org/zap"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	allTags = []string{AllTag}

	errRestrictedTag  = errors.New("restricted tag")
	errDuplicateCheck = errors.New("duplicated check")
)

type worker struct {
	log        logging.Logger
	namespace  string
	metrics    *metrics
	checksLock sync.RWMutex
	checks     map[string]*checker

	resultsLock                 sync.RWMutex
	results                     map[string]Result
	numFailingApplicationChecks int
	tags                        map[string]set.Set[string] // tag -> set of check names

	startOnce sync.Once
	closeOnce sync.Once
	wg        sync.WaitGroup
	closer    chan struct{}
}

type checker struct {
	checker            Checker
	isApplicationCheck bool
	tags               []string
}

func newWorker(
	log logging.Logger,
	namespace string,
	registerer prometheus.Registerer,
) (*worker, error) {
	metrics, err := newMetrics(namespace, registerer)
	return &worker{
		log:       log,
		namespace: namespace,
		metrics:   metrics,
		checks:    make(map[string]*checker),
		results:   make(map[string]Result),
		closer:    make(chan struct{}),
		tags:      make(map[string]set.Set[string]),
	}, err
}

func (w *worker) RegisterCheck(name string, check Checker, tags ...string) error {
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
	isApplicationCheck := applicationChecks.Contains(name)

	w.checks[name] = &checker{
		checker:            check,
		isApplicationCheck: isApplicationCheck,
		tags:               tags,
	}
	w.results[name] = notYetRunResult

	// Whenever a new check is added - it is failing
	w.log.Info("registered new check and initialized its state to failing",
		zap.String("namespace", w.namespace),
		zap.String("name", name),
		zap.Strings("tags", tags),
	)

	// If this is a new application-wide check, then all of the registered tags
	// now have one additional failing check.
	if isApplicationCheck {
		for tag := range w.tags {
			w.metrics.failingChecks.WithLabelValues(tag).Inc()
		}
		w.numFailingApplicationChecks++
		return nil
	}

	// Mark all the tags as failing
	for _, tag := range tags {
		gauge := w.metrics.failingChecks.WithLabelValues(tag)
		gauge.Inc()
		// If this is the first time this tag was registered, we also need to
		// account for the currently failing application-wide checks.
		if w.tags[tag].Len() == 1 {
			gauge.Add(float64(w.numFailingApplicationChecks))
		}
	}
	w.metrics.failingChecks.WithLabelValues(AllTag).Inc()
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
	tagSet := set.NewSet[string](len(tags) + 1)
	tagSet.Add(tags...)
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
		detachedCtx := utils.Detach(ctx)
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

func (w *worker) runCheck(ctx context.Context, wg *sync.WaitGroup, name string, check *checker) {
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
				zap.String("namespace", w.namespace),
				zap.String("name", name),
				zap.Strings("tags", check.tags),
				zap.Error(err),
			)
			if check.isApplicationCheck {
				for tag := range w.tags {
					w.metrics.failingChecks.WithLabelValues(tag).Inc()
				}
				w.numFailingApplicationChecks++
			} else {
				for _, tag := range check.tags {
					w.metrics.failingChecks.WithLabelValues(tag).Inc()
				}
				w.metrics.failingChecks.WithLabelValues(AllTag).Inc()
			}
		}
	} else if prevResult.Error != nil {
		w.log.Info("check started passing",
			zap.String("namespace", w.namespace),
			zap.String("name", name),
			zap.Strings("tags", check.tags),
			zap.Error(err),
		)
		if check.isApplicationCheck {
			for tag := range w.tags {
				w.metrics.failingChecks.WithLabelValues(tag).Dec()
			}
			w.numFailingApplicationChecks--
		} else {
			for _, tag := range check.tags {
				w.metrics.failingChecks.WithLabelValues(tag).Dec()
			}
			w.metrics.failingChecks.WithLabelValues(AllTag).Dec()
		}
	}
	w.results[name] = result
}
