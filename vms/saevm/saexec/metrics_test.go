// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	dto "github.com/prometheus/client_model/go"
)

func TestRollingGasThroughput(t *testing.T) {
	_, exec := newSUT(t)
	sut := exec.metrics.averageExecutedGasCharged

	at := func(d time.Duration) time.Time {
		return time.Unix(0, 0).Add(d)
	}
	const (
		sec = time.Second
		ms  = time.Millisecond
	)
	require.Equal(t, 5*sec, saeparams.Tau, "test values assume tau")

	steps := []struct {
		desc             string
		span             executionSpan
		wantHeadroom     float64
		wantGasPerSecond float64
	}{
		{
			desc: "first_block",
			span: executionSpan{
				start:    at(0),
				end:      at(sec),
				executed: 2e6,
				rate:     1e6,
			},
			wantHeadroom:     (2e6 / 1e6) / 1, // (gas-clock seconds = consumed/rate) / (wall-clock seconds)
			wantGasPerSecond: 2e6 / 1,         // consumed / (wall-clock seconds)
		},
		{
			desc: "second_block_contiguous_with_first",
			span: executionSpan{
				start:    at(sec),
				end:      at(2 * sec),
				executed: 0.5e6,
				rate:     1e6,
			},
			wantHeadroom:     (2e6/1e6 + 0.5e6/1e6) / (1 + 1),
			wantGasPerSecond: (2e6 + 0.5e6) / (1 + 1),
		},
		{
			desc: "idle_time_excluded_and_end_on_cutoff_evicted",
			span: executionSpan{
				start:    at(5500 * ms),
				end:      at(6 * sec), // cutoff == end of first block, which is evicted
				executed: 1e6,
				rate:     2e6,
			},
			wantHeadroom:     (0.5e6/1e6 + 1e6/2e6) / (1 + 0.5),
			wantGasPerSecond: (0.5e6 + 1e6) / (1 + 0.5),
		},
		{
			desc: "retain_straddling",
			span: executionSpan{
				start:    at(6 * sec),
				end:      at(6800 * ms), // cutoff partway through second block, which is kept
				executed: 4e6,
				rate:     3e6,
			},
			wantHeadroom:     (0.5e6/1e6 + 1e6/2e6 + 4e6/3e6) / (1 + 0.5 + 0.8),
			wantGasPerSecond: (0.5e6 + 1e6 + 4e6) / (1 + 0.5 + 0.8),
		},
		{
			desc: "evict_all_but_latest",
			span: executionSpan{
				start:    at(20 * sec),
				end:      at(23_500 * ms),
				executed: 7e6,
				rate:     2e6,
			},
			wantHeadroom:     (7e6 / 2e6) / 3.5,
			wantGasPerSecond: 7e6 / 3.5,
		},
	}

	var (
		n   uint64
		sum float64
	)
	const tolerance = 1e-9
	for _, s := range steps {
		t.Logf("%s: %s", s.desc, s.span.String())
		sut.observe(s.span)

		t.Run("headroom_histogram", func(t *testing.T) {
			var m dto.Metric
			require.NoErrorf(t, sut.headroom.Write(&m), "%T.headroom.Write(%T)", sut, &m)
			got := m.GetHistogram()

			n, sum = n+1, sum+s.wantHeadroom
			assert.Equal(t, n, got.GetSampleCount(), "sample count")
			assert.InDelta(t, sum, got.GetSampleSum(), tolerance, "sample sum")
		})

		assert.InDelta(t, s.wantHeadroom, testutil.ToFloat64(sut.latestHeadroom), tolerance, "latest headroom")
		assert.InDelta(t, s.wantGasPerSecond, testutil.ToFloat64(sut.gasPerSecond), tolerance, "gas per second")

		if t.Failed() {
			t.Skip("Non-independent steps will fail spuriously after earlier error")
		}
	}
}

func (s *executionSpan) String() string {
	comma := message.NewPrinter(language.English).Sprint
	epoch := time.Unix(0, 0)
	return fmt.Sprintf("%v to %v executed %s at %s/s", s.start.Sub(epoch), s.end.Sub(epoch), comma(s.executed), comma(s.rate))
}
