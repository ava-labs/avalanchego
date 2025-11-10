// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/coreth/cmd/simulator/metrics"
)

type THash interface {
	Hash() common.Hash
}

// TxSequence provides an interface to return a channel of transactions.
// The sequence is responsible for closing the channel when there are no further
// transactions.
type TxSequence[T THash] interface {
	Chan() <-chan T
}

// Worker defines the interface for issuance and confirmation of transactions.
// The caller is responsible for calling Close to cleanup resources used by the
// worker at the end of the simulation.
type Worker[T THash] interface {
	IssueTx(ctx context.Context, tx T) error
	ConfirmTx(ctx context.Context, tx T) error
	LatestHeight(ctx context.Context) (uint64, error)
}

// Execute the work of the given agent.
type Agent[T THash] interface {
	Execute(ctx context.Context) error
}

// issueNAgent issues and confirms a batch of N transactions at a time.
type issueNAgent[T THash] struct {
	sequence TxSequence[T]
	worker   Worker[T]
	n        uint64
	metrics  *metrics.Metrics
}

// NewIssueNAgent creates a new issueNAgent
func NewIssueNAgent[T THash](sequence TxSequence[T], worker Worker[T], n uint64, metrics *metrics.Metrics) Agent[T] {
	return &issueNAgent[T]{
		sequence: sequence,
		worker:   worker,
		n:        n,
		metrics:  metrics,
	}
}

// Execute issues txs in batches of N and waits for them to confirm
func (a issueNAgent[T]) Execute(ctx context.Context) error {
	if a.n == 0 {
		return errors.New("batch size n cannot be equal to 0")
	}

	txChan := a.sequence.Chan()
	confirmedCount := 0
	batchI := 0
	m := a.metrics
	txMap := make(map[common.Hash]time.Time)

	// Tracks the total amount of time waiting for issuing and confirming txs
	var (
		totalIssuedTime    time.Duration
		totalConfirmedTime time.Duration
	)

	// Start time for execution
	start := time.Now()
	for {
		var (
			txs     = make([]T, 0, a.n)
			tx      T
			moreTxs bool
		)
		// Start issuance batch
		issuedStart := time.Now()
	L:
		for i := uint64(0); i < a.n; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case tx, moreTxs = <-txChan:
				if !moreTxs {
					break L
				}
				issuanceIndividualStart := time.Now()
				txMap[tx.Hash()] = issuanceIndividualStart
				if err := a.worker.IssueTx(ctx, tx); err != nil {
					return fmt.Errorf("failed to issue transaction %d: %w", len(txs), err)
				}
				issuanceIndividualDuration := time.Since(issuanceIndividualStart)
				m.IssuanceTxTimes.Observe(issuanceIndividualDuration.Seconds())
				txs = append(txs, tx)
			}
		}
		// Get the batch's issuance time and add it to totalIssuedTime
		issuedDuration := time.Since(issuedStart)
		log.Info("Issuance Batch Done", "batch", batchI, "time", issuedDuration.Seconds())
		totalIssuedTime += issuedDuration

		// Wait for txs in this batch to confirm
		confirmedStart := time.Now()
		for i, tx := range txs {
			confirmedIndividualStart := time.Now()
			if err := a.worker.ConfirmTx(ctx, tx); err != nil {
				return fmt.Errorf("failed to await transaction %d: %w", i, err)
			}
			confirmationIndividualDuration := time.Since(confirmedIndividualStart)
			issuanceToConfirmationIndividualDuration := time.Since(txMap[tx.Hash()])
			m.ConfirmationTxTimes.Observe(confirmationIndividualDuration.Seconds())
			m.IssuanceToConfirmationTxTimes.Observe(issuanceToConfirmationIndividualDuration.Seconds())
			delete(txMap, tx.Hash())
			confirmedCount++
		}
		// Get the batch's confirmation time and add it to totalConfirmedTime
		confirmedDuration := time.Since(confirmedStart)
		log.Info("Confirmed Batch Done", "batch", batchI, "time", confirmedDuration.Seconds())
		totalConfirmedTime += confirmedDuration

		// Check if this is the last batch, if so write the final log and return
		if !moreTxs {
			totalTime := time.Since(start).Seconds()
			log.Info("Execution complete", "totalTxs", confirmedCount, "totalTime", totalTime, "TPS", float64(confirmedCount)/totalTime,
				"issuanceTime", totalIssuedTime.Seconds(), "confirmedTime", totalConfirmedTime.Seconds())

			return nil
		}

		batchI++
	}
}
