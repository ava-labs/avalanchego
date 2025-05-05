// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package orchestrate

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/tests/load/agent"
)

// BurstOrchestrator executes a series of worker/tx sequence pairs.
// Each worker/txSequence pair issues [batchSize] transactions, confirms all
// of them as accepted, and then moves to the next batch until the txSequence
// is exhausted.
type BurstOrchestrator[T, U comparable] struct {
	agents  []*agent.Agent[T, U]
	timeout time.Duration
}

func NewBurstOrchestrator[T, U comparable](
	agents []*agent.Agent[T, U],
	timeout time.Duration,
) *BurstOrchestrator[T, U] {
	return &BurstOrchestrator[T, U]{
		agents:  agents,
		timeout: timeout,
	}
}

func (o *BurstOrchestrator[T, U]) Execute(ctx context.Context) error {
	observerCtx, observerCancel := context.WithCancel(ctx)
	defer observerCancel()

	// start a goroutine to confirm each issuer's transactions
	observerGroup := errgroup.Group{}
	for _, agent := range o.agents {
		observerGroup.Go(func() error { return agent.Listener.Listen(observerCtx) })
	}

	const logInterval = 10 * time.Second
	logTicker := time.NewTicker(logInterval)

	// start issuing transactions sequentially from each issuer
	issuerGroup := errgroup.Group{}
	for _, agent := range o.agents {
		issuerGroup.Go(func() error {
			for range agent.TxTarget {
				tx, err := agent.Generator.GenerateTx()
				if err != nil {
					return fmt.Errorf("generating transaction: %w", err)
				}
				if err := agent.Issuer.IssueTx(ctx, tx); err != nil {
					return fmt.Errorf("issuing transaction: %w", err)
				}

				agent.Listener.RegisterIssued(tx)

				select {
				case <-logTicker.C:
					agent.Tracker.Log()
				default:
				}
			}
			return nil
		})
	}

	// wait for all issuers to finish sending their transactions
	if err := issuerGroup.Wait(); err != nil {
		return fmt.Errorf("issuers: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	// start a goroutine that will cancel the observer group's context
	// if either the parent context is cancelled or our timeout elapses
	go func() {
		<-ctx.Done()
		observerCancel()
	}()

	// blocks until either all of the observers have finished or our context
	// is cancelled signalling for early termination (with an error)
	if err := observerGroup.Wait(); err != nil {
		return fmt.Errorf("observers: %w", err)
	}
	return nil
}
