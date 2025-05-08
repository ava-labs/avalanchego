// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ orchestrator = (*BurstOrchestrator[any])(nil)

type BurstOrchestratorConfig struct {
	TxsPerIssuer uint64
	Timeout      time.Duration
}

// BurstOrchestrator tests the network by sending a fixed number of
// transactions en masse in a short timeframe.
type BurstOrchestrator[T any] struct {
	agents []Agent[T]

	config BurstOrchestratorConfig
}

func NewBurstOrchestrator[T any](
	agents []Agent[T],
	log logging.Logger,
	config BurstOrchestratorConfig,
) (*BurstOrchestrator[T], error) {
	return &BurstOrchestrator[T]{
		agents: agents,
		config: config,
	}, nil
}

// Execute orders issuers to send a fixed number of transactions and then waits
// for all of their statuses to be confirmed or for a timeout to occur.
func (o *BurstOrchestrator[T]) Execute(ctx context.Context) error {
	observerCtx, observerCancel := context.WithCancel(ctx)
	defer observerCancel()

	// start a goroutine to confirm each issuer's transactions
	observerGroup := errgroup.Group{}
	for _, agent := range o.agents {
		observerGroup.Go(func() error { return agent.Listener.Listen(observerCtx) })
	}

	// start issuing transactions sequentially from each issuer
	issuerGroup := errgroup.Group{}
	for _, agent := range o.agents {
		issuerGroup.Go(func() error {
			defer agent.Listener.IssuingDone()
			for range o.config.TxsPerIssuer {
				tx, err := agent.Issuer.GenerateAndIssueTx(ctx)
				if err != nil {
					return err
				}
				agent.Listener.RegisterIssued(tx)
			}
			return nil
		})
	}

	// wait for all issuers to finish sending their transactions
	if err := issuerGroup.Wait(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, o.config.Timeout)
	defer cancel()

	// start a goroutine that will cancel the observer group's context
	// if either the parent context is cancelled or our timeout elapses
	go func() {
		<-ctx.Done()
		observerCancel()
	}()

	// blocks until either all of the issuers have finished or our context
	// is cancelled signalling for early termination (with an error)
	return observerGroup.Wait()
}
