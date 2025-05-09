// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	_ orchestrator = (*GradualOrchestrator[any])(nil)

	ErrFailedToReachTargetTPS = errors.New("failed to reach target TPS")
)

type GradualOrchestratorConfig struct {
	// The maximum TPS the orchestrator should aim for.
	//
	// If set to -1, the orchestrator will behave in a burst fashion, instead
	// sending MinTPS transactions at once and waiting sustainedTime seconds
	// before checking if MinTPS transactions were confirmed as accepted.
	MaxTPS int64
	// The minimum TPS the orchestrator should start with.
	MinTPS int64
	// The step size to increase the TPS by.
	Step int64

	// The factor by which to pad the number of txs an issuer sends per second
	// for example, if targetTPS = 1000 and numIssuers = 10, then each issuer
	// will send (1000/10)*TxRateMultiplier transactions per second.
	//
	// Maintaining a multiplier above target provides a toggle to keep load
	// persistently above instead of below target. This ensures load generation
	// does not pause issuance at the target and persistently under-issue and
	// fail to account for the time it takes to add more load.
	TxRateMultiplier float64

	// The time period which TPS is averaged over
	// Similarly, the time period which the orchestrator will wait before
	// computing the average TPS.
	SustainedTime time.Duration
	// The number of attempts to try achieving a given target TPS before giving up.
	MaxAttempts uint64

	// Whether the orchestrator should return if the maxTPS has been reached
	Terminate bool
}

func NewGradualOrchestratorConfig() GradualOrchestratorConfig {
	return GradualOrchestratorConfig{
		MaxTPS:           5_000,
		MinTPS:           1_000,
		Step:             1_000,
		TxRateMultiplier: 1.3,
		SustainedTime:    20 * time.Second,
		MaxAttempts:      3,
		Terminate:        true,
	}
}

// GradualOrchestrator tests the network by continuously sending
// transactions at a given rate (currTargetTPS) and increasing that rate until it detects that
// the network can no longer make progress (i.e. the rate at the network accepts
// transactions is less than currTargetTPS).
type GradualOrchestrator[T comparable] struct {
	agents  []Agent[T]
	tracker *Tracker[T]

	log logging.Logger

	maxObservedTPS atomic.Uint64

	observerGroup *errgroup.Group
	issuerGroup   *errgroup.Group

	config GradualOrchestratorConfig
}

func NewGradualOrchestrator[T comparable](
	agents []Agent[T],
	tracker *Tracker[T],
	log logging.Logger,
	config GradualOrchestratorConfig,
) *GradualOrchestrator[T] {
	return &GradualOrchestrator[T]{
		agents:  agents,
		tracker: tracker,
		log:     log,
		config:  config,
	}
}

func (o *GradualOrchestrator[T]) Execute(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)

	// start a goroutine to confirm each issuer's transactions
	o.observerGroup = &errgroup.Group{}
	for _, agent := range o.agents {
		o.observerGroup.Go(func() error { return agent.Listener.Listen(ctx) })
	}

	// start the test and block until it's done
	success := o.run(ctx)

	var err error
	if !success {
		err = ErrFailedToReachTargetTPS
	}

	// stop the observers and issuers
	cancel()

	// block until both the observers and issuers have stopped
	return errors.Join(o.issuerGroup.Wait(), o.observerGroup.Wait(), err)
}

// run the gradual load test by continuously increasing the rate at which
// transactions are sent
//
// run blocks until one of the following conditions is met:
//
// 1. an issuer has errored
// 2. the max TPS target has been reached and we can terminate
// 3. the maximum number of attempts to reach a target TPS has been reached
func (o *GradualOrchestrator[T]) run(ctx context.Context) bool {
	var (
		prevConfirmed        = o.tracker.GetObservedConfirmed()
		prevTime             = time.Now()
		currTargetTPS        = new(atomic.Int64)
		attempts      uint64 = 1
		// true if the orchestrator has reached the max TPS target
		achievedTargetTPS bool
	)

	currTargetTPS.Store(o.config.MinTPS)

	issuerGroup, issuerCtx := errgroup.WithContext(ctx)
	o.issuerGroup = issuerGroup

	o.issueTxs(issuerCtx, currTargetTPS)

	for {
		// wait for the sustained time to pass or for the context to be cancelled
		select {
		case <-time.After(o.config.SustainedTime):
		case <-issuerCtx.Done(): // the parent context was cancelled or an issuer errored
		}

		if issuerCtx.Err() != nil {
			break // Case 1
		}

		currConfirmed := o.tracker.GetObservedConfirmed()
		currTime := time.Now()

		if o.config.MaxTPS == -1 {
			return currTargetTPS.Load() == int64(currConfirmed)
		}

		tps := computeTPS(prevConfirmed, currConfirmed, currTime.Sub(prevTime))
		o.setMaxObservedTPS(tps)

		// if max TPS target has been reached and we don't terminate, then continue here
		// so we do not keep increasing the target TPS
		if achievedTargetTPS && !o.config.Terminate {
			o.log.Info(
				"current network state",
				zap.Uint64("current TPS", tps),
				zap.Uint64("max observed TPS", o.maxObservedTPS.Load()),
			)
			continue
		}

		if int64(tps) >= currTargetTPS.Load() {
			if currTargetTPS.Load() >= o.config.MaxTPS {
				achievedTargetTPS = true
				o.log.Info(
					"max TPS target reached",
					zap.Int64("max TPS target", currTargetTPS.Load()),
					zap.Uint64("average TPS", tps),
				)
				if o.config.Terminate {
					o.log.Info("terminating orchestrator")
					break // Case 2
				}
				o.log.Info("orchestrator will now continue running at max TPS")
				continue
			}
			o.log.Info(
				"increasing TPS",
				zap.Int64("previous target TPS", currTargetTPS.Load()),
				zap.Uint64("average TPS", tps),
				zap.Int64("new target TPS", currTargetTPS.Load()+o.config.Step),
			)
			currTargetTPS.Add(o.config.Step)
			attempts = 1
		} else {
			if attempts >= o.config.MaxAttempts {
				o.log.Info(
					"max attempts reached",
					zap.Int64("attempted target TPS", currTargetTPS.Load()),
					zap.Uint64("number of attempts", attempts),
				)
				break // Case 3
			}
			o.log.Info(
				"failed to reach target TPS, retrying",
				zap.Int64("current target TPS", currTargetTPS.Load()),
				zap.Uint64("average TPS", tps),
				zap.Uint64("attempt number", attempts),
			)
			attempts++
		}

		prevConfirmed = currConfirmed
		prevTime = currTime
	}

	return achievedTargetTPS
}

// GetObservedIssued returns the max TPS the orchestrator observed
func (o *GradualOrchestrator[T]) GetMaxObservedTPS() uint64 {
	return o.maxObservedTPS.Load()
}

// start a goroutine to each issuer to continuously send transactions.
// if an issuer errors, all other issuers will stop as well.
func (o *GradualOrchestrator[T]) issueTxs(ctx context.Context, currTargetTPS *atomic.Int64) {
	for _, agent := range o.agents {
		o.issuerGroup.Go(func() error {
			for {
				if ctx.Err() != nil {
					return nil //nolint:nilerr
				}
				currTime := time.Now()
				txsPerIssuer := uint64(math.Ceil(float64(currTargetTPS.Load())/float64(len(o.agents))) * o.config.TxRateMultiplier)
				// always listen until listener context is cancelled, do not call agent.Listener.IssuingDone().
				for range txsPerIssuer {
					tx, err := agent.Issuer.GenerateAndIssueTx(ctx)
					if err != nil {
						return err
					}
					agent.Listener.RegisterIssued(tx)
				}

				if o.config.MaxTPS == -1 {
					agent.Listener.IssuingDone()
					return nil
				}

				diff := time.Second - time.Since(currTime)
				if diff <= 0 {
					continue
				}
				timer := time.NewTimer(diff)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil
				case <-timer.C:
				}
			}
		})
	}
}

// setMaxObservedTPS only if tps > the current max observed TPS.
func (o *GradualOrchestrator[T]) setMaxObservedTPS(tps uint64) {
	if tps > o.maxObservedTPS.Load() {
		o.maxObservedTPS.Store(tps)
	}
}

func computeTPS(initial uint64, final uint64, duration time.Duration) uint64 {
	if duration <= 0 {
		return 0
	}

	numTxs := final - initial
	tps := float64(numTxs) / duration.Seconds()

	return uint64(tps)
}
