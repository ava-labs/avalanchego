// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/utils/logging"
)

const pingFrequency = 500 * time.Millisecond

type Orchestrator struct {
	senders  []*Sender
	builders []Builder
	tracker  *Tracker
	log      logging.Logger
}

func NewOrchestrator(
	senders []*Sender,
	builders []Builder,
	tracker *Tracker,
	log logging.Logger,
) *Orchestrator {
	return &Orchestrator{
		senders:  senders,
		builders: builders,
		tracker:  tracker,
		log:      log,
	}
}

func (o *Orchestrator) Run(ctx context.Context) error {
	o.log.Debug("starting run")
	issuanceF := func(issuanceDuration time.Duration) {
		o.tracker.LogIssuance(issuanceDuration)
	}

	confirmationF := func(receipt *types.Receipt, confirmationDuration time.Duration) {
		o.tracker.LogConfirmation(receipt, confirmationDuration)
	}

	eg, childCtx := errgroup.WithContext(ctx)

	for i := range o.senders {
		eg.Go(func() error {
			for {
				select {
				case <-childCtx.Done():
					return nil
				default:
				}

				if err := o.senders[i].SendTx(
					childCtx,
					o.builders[i],
					pingFrequency,
					issuanceF,
					confirmationF,
				); err != nil {
					return err
				}
			}
		})
	}

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	prevTotalGasUsed := o.tracker.TotalGasUsed()
	prevTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		currTotalGasUsed := o.tracker.TotalGasUsed()
		currTime := time.Now()

		gps := computeTPS(prevTotalGasUsed, currTotalGasUsed, currTime.Sub(prevTime))
		o.log.Info("stats", zap.Uint64("gps", gps))

		prevTime = currTime
		prevTotalGasUsed = currTotalGasUsed
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
