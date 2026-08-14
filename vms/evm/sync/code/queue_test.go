// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"errors"
	"sync"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

// A closed queue releases a waiter at once, so a batcher that has drained
// everything cannot sleep through its own shutdown.
func TestQueue_ClosedReleasesWaiter(t *testing.T) {
	t.Parallel()

	q := newQueue()
	q.close()
	require.NoError(t, q.wait(t.Context()))
}

// The batcher stops on empty and closed, so that pair must mean nothing accepted
// is still queued. An admit either queues every hash it was given or none.
func TestQueue_DrainedMeansEmpty(t *testing.T) {
	t.Parallel()

	errWrite := errors.New("write failed")

	tests := []struct {
		name         string
		rounds       int
		producers    int
		perAdmit     int
		writeErr     error
		wantAccepted bool // whether any admit is expected to succeed at all
	}{
		{name: "one_producer", rounds: 20000, producers: 1, perAdmit: 1, wantAccepted: true},
		{name: "many_producers", rounds: 2000, producers: 8, perAdmit: 1, wantAccepted: true},
		{name: "batched_admit", rounds: 2000, producers: 4, perAdmit: 5, wantAccepted: true},
		{name: "write_fails", rounds: 100, producers: 4, perAdmit: 3, writeErr: errWrite},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			offered := make([]common.Hash, tt.perAdmit)
			for i := range offered {
				offered[i] = common.Hash{byte(i + 1)}
			}

			everAccepted := false
			for round := range tt.rounds {
				q := newQueue()

				// Buffered to the producer count, so no admit waits to report.
				results := make(chan error, tt.producers)

				var wg sync.WaitGroup
				for range tt.producers {
					wg.Go(func() {
						results <- q.admit(offered, func() error { return tt.writeErr })
					})
				}
				wg.Go(q.close)

				// Sleeping on an empty drain keeps this from starving the producers.
				drained := 0
				for {
					taken, closed := q.take()
					drained += len(taken)
					if len(taken) > 0 {
						continue
					}
					if closed {
						break
					}
					require.NoError(t, q.wait(t.Context()))
				}
				wg.Wait()
				close(results)

				accepted := 0
				for err := range results {
					if err == nil {
						accepted += len(offered)
						continue
					}
					require.Truef(t, errors.Is(err, ErrInputClosed) || errors.Is(err, tt.writeErr),
						"round %d: admit refused with an unexpected error: %v", round, err)
				}

				require.Equalf(t, accepted, drained,
					"round %d: a hash stayed queued after the drain reported empty and closed", round)
				everAccepted = everAccepted || accepted > 0
			}

			require.Equal(t, tt.wantAccepted, everAccepted,
				"whether any admit is accepted is what the case is built to exercise")
		})
	}
}
