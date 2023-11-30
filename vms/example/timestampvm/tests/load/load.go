// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// load implements the load tests.
package load_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/ava-labs/timestampvm/tests/load/client"
	"github.com/ava-labs/timestampvm/timestampvm"
	log "github.com/inconshreveable/log15"
	"golang.org/x/sync/errgroup"
)

const (
	backoffDur = 50 * time.Millisecond
	maxBackoff = 5 * time.Second
)

var _ Worker = (*timestampvmLoadWorker)(nil)

type Worker interface {
	// Name returns the name of the worker
	Name() string
	// AddLoad adds load to the underlying network of the worker.
	// Terminates without an error if the quit channel is closed.
	AddLoad(ctx context.Context, quit <-chan struct{}) error
	// GetLastAcceptedHeight returns the height of the last accepted block.
	GetLastAcceptedHeight(ctx context.Context) (uint64, error)
}

// timestampvmLoadWorker implements the LoadWorker interface and adds continuous load through its client.
type timestampvmLoadWorker struct {
	uri string
	client.Client
}

func newLoadWorkers(uris []string, blockchainID string) []Worker {
	workers := make([]Worker, 0, len(uris))
	for _, uri := range uris {
		workers = append(workers, newLoadWorker(uri, blockchainID))
	}

	return workers
}

func newLoadWorker(uri string, blockchainID string) *timestampvmLoadWorker {
	uri = fmt.Sprintf("%s/ext/bc/%s", uri, blockchainID)
	return &timestampvmLoadWorker{
		uri:    uri,
		Client: client.New(uri),
	}
}

func (t *timestampvmLoadWorker) Name() string {
	return fmt.Sprintf("TimestampVM RPC Worker %s", t.uri)
}

func (t *timestampvmLoadWorker) AddLoad(ctx context.Context, quit <-chan struct{}) error {
	delay := time.Duration(0)
	for {
		select {
		case <-quit:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("%s finished: %w", t.Name(), ctx.Err())
		default:
		}

		data := [timestampvm.DataLen]byte{}
		_, err := rand.Read(data[:])
		if err != nil {
			return fmt.Errorf("failed to read random data: %w", err)
		}
		success, err := t.ProposeBlock(ctx, data)
		if err != nil {
			return fmt.Errorf("%s failed: %w", t.Name(), err)
		}
		if success && delay > 0 {
			delay -= backoffDur
		} else if !success && delay < maxBackoff {
			// If the mempool is full, pause before submitting more data
			//
			// TODO: in a robust testing scenario, we'd want to resubmit this
			// data to avoid loss
			delay += backoffDur
		}
		time.Sleep(delay)
	}
}

// GetLastAcceptedHeight returns the height of the last accepted block according to the worker
func (t *timestampvmLoadWorker) GetLastAcceptedHeight(ctx context.Context) (uint64, error) {
	_, _, lastHeight, _, _, err := t.GetBlock(ctx, nil)
	return lastHeight, err
}

// RunLoadTest runs a load test on the workers
// Assumes it's safe to call a worker in parallel
func RunLoadTest(ctx context.Context, workers []Worker, terminalHeight uint64, maxDuration time.Duration) error {
	if len(workers) == 0 {
		return fmt.Errorf("cannot run a load test with %d workers", len(workers))
	}

	var (
		quit        = make(chan struct{})
		cancel      context.CancelFunc
		start       = time.Now()
		worker      = workers[0]
		startHeight = uint64(0)
		err         error
	)

	for {
		startHeight, err = worker.GetLastAcceptedHeight(ctx)
		if err != nil {
			log.Warn("Failed to get last accepted height", "err", err, "worker", worker.Name())
			time.Sleep(3 * time.Second)
			continue
		}
		break
	}
	log.Info("Running load test", "numWorkers", len(workers), "terminalHeight", terminalHeight, "maxDuration", maxDuration)

	if maxDuration != 0 {
		ctx, cancel = context.WithTimeout(ctx, maxDuration)
		defer cancel()
	}

	eg, egCtx := errgroup.WithContext(ctx)
	for _, worker := range workers {
		worker := worker
		eg.Go(func() error {
			if err := worker.AddLoad(egCtx, quit); err != nil {
				return fmt.Errorf("worker %q failed while adding load: %w", worker.Name(), err)
			}

			return nil
		})
	}

	eg.Go(func() error {
		last := startHeight
		for egCtx.Err() == nil {
			lastHeight, err := worker.GetLastAcceptedHeight(egCtx)
			if err != nil {
				continue
			}
			log.Info("Stats", "height", lastHeight,
				"avg bps", float64(lastHeight)/time.Since(start).Seconds(),
				"last bps", float64(lastHeight-last)/3.0,
			)
			if lastHeight > terminalHeight {
				log.Info("exiting at terminal height")
				close(quit)
				return nil
			}
			last = lastHeight
			time.Sleep(3 * time.Second)
		}

		return fmt.Errorf("failed to reach terminal height: %w", egCtx.Err())
	})

	return eg.Wait()
}
