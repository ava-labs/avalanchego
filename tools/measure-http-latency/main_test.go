package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSummarize(t *testing.T) {
	t.Parallel()

	report := summarize(
		"https://example.com",
		[]sample{
			{Connect: 10 * time.Millisecond, TLS: 20 * time.Millisecond, TTFB: 30 * time.Millisecond, Total: 40 * time.Millisecond},
			{Connect: 30 * time.Millisecond, TLS: 40 * time.Millisecond, TTFB: 50 * time.Millisecond, Total: 60 * time.Millisecond},
		},
		map[int]int{200: 2},
	)

	require.Equal(t, "https://example.com", report.URL)
	require.Equal(t, 2, report.Samples)
	require.Equal(t, 20.0, report.AvgConnectMS)
	require.Equal(t, 30.0, report.AvgTLSMS)
	require.Equal(t, 40.0, report.AvgTTFBMS)
	require.Equal(t, 50.0, report.AvgTotalMS)
	require.Equal(t, map[int]int{200: 2}, report.StatusCodes)
}

func TestWriteTextReport(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	writeTextReport(&buffer, report{
		URL:          "https://example.com",
		Samples:      2,
		StatusCodes:  map[int]int{401: 2},
		AvgConnectMS: 88.4,
		AvgTLSMS:     116.2,
		AvgTTFBMS:    232.1,
		AvgTotalMS:   233.4,
	})

	require.Equal(t, "url=https://example.com\nsamples=2\nhttp_code[401]=2\navg_connect=88.4ms avg_tls=116.2ms avg_ttfb=232.1ms avg_total=233.4ms\n", buffer.String())
}
