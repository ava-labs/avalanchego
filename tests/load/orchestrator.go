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

var ErrFailedToReachTargetTPS = errors.New("failed to reach target TPS")

type TxID interface {
	~[32]byte
}

type Issuer[T TxID] interface {
	// GenerateAndIssueTx generates and sends a tx to the network, and informs the
	// tracker that it sent said transaction. It returns the sent transaction.
	GenerateAndIssueTx(ctx context.Context) (T, error)
}

type Listener[T TxID] interface {
	// Listen for the final status of transactions and notify the tracker
	// Listen stops if the context is done, an error occurs, or if it received
	// all the transactions issued and the issuer no longer issues any.
	// Listen MUST return a nil error if the context is canceled.
	Listen(ctx context.Context) error

	// RegisterIssued informs the listener that a transaction was issued.
	RegisterIssued(txID T)

	// IssuingDone informs the listener that no more transactions will be issued.
	IssuingDone()
}

type OrchestratorConfig struct {
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

// NewOrchestratorConfig returns a default OrchestratorConfig with pre-set parameters
// for gradual load testing.
func NewOrchestratorConfig() OrchestratorConfig {
	return OrchestratorConfig{
		MaxTPS:           5_000,
		MinTPS:           1_000,
		Step:             1_000,
		TxRateMultiplier: 1.3,
		SustainedTime:    20 * time.Second,
		MaxAttempts:      3,
		Terminate:        true,
	}
}

// Orchestrator tests the network by continuously sending
// transactions at a given rate (currTargetTPS) and increasing that rate until it detects that
// the network can no longer make progress (i.e. the rate at the network accepts
// transactions is less than currTargetTPS).
type Orchestrator[T TxID] struct {
	agents  []Agent[T]
	tracker *Tracker[T]

	log logging.Logger

	maxObservedTPS atomic.Int64

	observerGroup *errgroup.Group
	issuerGroup   *errgroup.Group

	config OrchestratorConfig
}

func NewOrchestrator[T TxID](
	agents []Agent[T],
	tracker *Tracker[T],
	log logging.Logger,
	config OrchestratorConfig,
) *Orchestrator[T] {
	return &Orchestrator[T]{
		agents:  agents,
		tracker: tracker,
		log:     log,
		config:  config,
	}
}

func (o *Orchestrator[_]) Execute(ctx context.Context) error {
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

// run the load test by continuously increasing the rate at which
// transactions are sent
//
// run blocks until one of the following conditions is met:
//
// 1. an issuer has errored
// 2. the max TPS target has been reached and we can terminate
// 3. the maximum number of attempts to reach a target TPS has been reached
func (o *Orchestrator[_]) run(ctx context.Context) bool {
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
		o.setMaxObservedTPS(int64(tps))

		// if max TPS target has been reached and we don't terminate, then continue here
		// so we do not keep increasing the target TPS
		if achievedTargetTPS && !o.config.Terminate {
			o.log.Info(
				"current network state",
				zap.Uint64("current TPS", tps),
				zap.Int64("max observed TPS", o.maxObservedTPS.Load()),
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
func (o *Orchestrator[_]) GetMaxObservedTPS() int64 {
	return o.maxObservedTPS.Load()
}

// start a goroutine to each issuer to continuously send transactions.
// if an issuer errors, all other issuers will stop as well.
func (o *Orchestrator[_]) issueTxs(ctx context.Context, currTargetTPS *atomic.Int64) {
	for _, agent := range o.agents {
		o.issuerGroup.Go(func() error {
			for {
				if ctx.Err() != nil {
					return nil //nolint:nilerr
				}
				currTime := time.Now()
				targetTPSPerAgent := math.Ceil(float64(currTargetTPS.Load()) / float64(len(o.agents)))
				txsPerIssuer := uint64(targetTPSPerAgent * o.config.TxRateMultiplier)
				if txsPerIssuer == uint64(targetTPSPerAgent) && o.config.TxRateMultiplier > 1 {
					// For low target TPS and rate multiplier, the rate multiplier can have no effect.
					// For example, for 50 TPS target, 5 agents, and rate multiplier of 1.05, we have
					// uint64(10*1.05) = 10 therefore we add 1 to force an increase in txs issued.
					txsPerIssuer++
				}
				// always listen until listener context is cancelled, do not call agent.Listener.IssuingDone().
				for range txsPerIssuer {
					txID, err := agent.Issuer.GenerateAndIssueTx(ctx)
					if err != nil {
						return err
					}
					o.tracker.Issue(txID)
					agent.Listener.RegisterIssued(txID)
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
func (o *Orchestrator[_]) setMaxObservedTPS(tps int64) {
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
