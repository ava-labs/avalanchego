// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSummarize(t *testing.T) {
	t.Parallel()

	report := summarize(
		"https://example.com",
		"warm-h2",
		[]sample{
			{Connect: 10 * time.Millisecond, TLS: 20 * time.Millisecond, TTFB: 30 * time.Millisecond, WriteToFirstByte: 5 * time.Millisecond, Total: 40 * time.Millisecond},
			{Connect: 30 * time.Millisecond, TLS: 40 * time.Millisecond, TTFB: 50 * time.Millisecond, WriteToFirstByte: 7 * time.Millisecond, Total: 60 * time.Millisecond},
		},
		2,
		map[int]int{200: 2},
	)

	require.Equal(t, "https://example.com", report.URL)
	require.Equal(t, "warm-h2", report.Mode)
	require.Equal(t, 2, report.Samples)
	require.Equal(t, 2, report.ReusedSamples)
	require.Equal(t, 20.0, report.AvgConnectMS)
	require.Equal(t, 30.0, report.AvgTLSMS)
	require.Equal(t, 40.0, report.AvgTTFBMS)
	require.Equal(t, 6.0, report.AvgWriteToFirstByteMS)
	require.Equal(t, 50.0, report.AvgTotalMS)
	require.Equal(t, map[int]int{200: 2}, report.StatusCodes)
}

func TestWriteTextReport(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	writeTextReport(&buffer, report{
		URL:                   "https://example.com",
		Mode:                  "warm-h2",
		Samples:               2,
		ReusedSamples:         2,
		StatusCodes:           map[int]int{401: 2},
		AvgConnectMS:          88.4,
		AvgTLSMS:              116.2,
		AvgTTFBMS:             232.1,
		AvgWriteToFirstByteMS: 12.3,
		AvgTotalMS:            233.4,
	})

	require.Equal(t, "url=https://example.com\nmode=warm-h2\nsamples=2\nreused_samples=2\nhttp_code[401]=2\navg_connect=88.4ms avg_tls=116.2ms avg_ttfb=232.1ms avg_write_to_first_byte=12.3ms avg_total=233.4ms\n", buffer.String())
}

func TestMeasureDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusMovedPermanently)
	}))
	defer redirect.Close()

	report, err := measure(config{url: redirect.URL, samples: 1, timeout: 5 * time.Second, format: "json", mode: "cold"})
	require.NoError(t, err)
	require.Equal(t, map[int]int{http.StatusMovedPermanently: 1}, report.StatusCodes)
}

func TestMeasureWarmH2UsesReusedConnections(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	report, err := measure(config{
		url:     server.URL,
		samples: 2,
		timeout: 5 * time.Second,
		format:  "json",
		mode:    "warm-h2",
	})
	require.NoError(t, err)
	require.Equal(t, 2, report.Samples)
	require.Equal(t, 2, report.ReusedSamples)
	require.Equal(t, map[int]int{http.StatusNoContent: 2}, report.StatusCodes)
	require.GreaterOrEqual(t, report.AvgWriteToFirstByteMS, 0.0)
}

func TestParseFlagsRejectsInvalidMode(t *testing.T) {
	t.Parallel()

	_, err := parseFlags([]string{"--url=https://example.com", "--mode=bad"})
	require.ErrorContains(t, err, "--mode must be one of: cold, warm-h2")
}
