// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	modeCold   = "cold"
	modeWarmH2 = "warm-h2"
)

var errInvalidMode = errors.New("invalid mode")

type config struct {
	url     string
	samples int
	timeout time.Duration
	format  string
	mode    string
}

type sample struct {
	StatusCode       int           `json:"statusCode"`
	Connect          time.Duration `json:"-"`
	TLS              time.Duration `json:"-"`
	TTFB             time.Duration `json:"-"`
	WriteToFirstByte time.Duration `json:"-"`
	Total            time.Duration `json:"-"`
	ReusedConnection bool          `json:"reusedConnection"`
}

type report struct {
	URL                   string      `json:"url"`
	Mode                  string      `json:"mode"`
	Samples               int         `json:"samples"`
	ReusedSamples         int         `json:"reusedSamples"`
	StatusCodes           map[int]int `json:"statusCodes"`
	AvgConnectMS          float64     `json:"avgConnectMs"`
	AvgTLSMS              float64     `json:"avgTlsMs"`
	AvgTTFBMS             float64     `json:"avgTtfbMs"`
	AvgWriteToFirstByteMS float64     `json:"avgWriteToFirstByteMs"`
	AvgTotalMS            float64     `json:"avgTotalMs"`
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	report, err := measure(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	switch cfg.format {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "error: encode json: %v\n", err)
			os.Exit(1)
		}
	case "text":
		writeTextReport(os.Stdout, report)
	default:
		fmt.Fprintf(os.Stderr, "error: unsupported format %q\n", cfg.format)
		os.Exit(1)
	}
}

func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("measure-http-latency", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	cfg := config{}
	fs.StringVar(&cfg.url, "url", "", "HTTP URL to measure")
	fs.IntVar(&cfg.samples, "samples", 10, "number of samples to collect")
	fs.DurationVar(&cfg.timeout, "timeout", 10*time.Second, "per-request timeout")
	fs.StringVar(&cfg.format, "format", "text", "output format: text or json")
	fs.StringVar(&cfg.mode, "mode", modeCold, "measurement mode: cold or warm-h2")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.url == "" {
		return config{}, errors.New("--url is required")
	}
	if cfg.samples <= 0 {
		return config{}, errors.New("--samples must be > 0")
	}
	cfg.format = strings.ToLower(cfg.format)
	if cfg.format != "text" && cfg.format != "json" {
		return config{}, errors.New("--format must be one of: text, json")
	}
	cfg.mode = strings.ToLower(cfg.mode)
	if cfg.mode != modeCold && cfg.mode != modeWarmH2 {
		return config{}, fmt.Errorf("%w: %q (must be one of: %s, %s)", errInvalidMode, cfg.mode, modeCold, modeWarmH2)
	}
	if cfg.timeout <= 0 {
		return config{}, errors.New("--timeout must be > 0")
	}
	return cfg, nil
}

func measure(cfg config) (report, error) {
	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DisableKeepAlives: cfg.mode == modeCold,
		ForceAttemptHTTP2: cfg.mode == modeWarmH2,
	}
	client := &http.Client{
		Timeout:   cfg.timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	samples := make([]sample, 0, cfg.samples)
	statusCodes := make(map[int]int)
	reusedSamples := 0

	attempts := cfg.samples
	if cfg.mode == modeWarmH2 {
		attempts += cfg.samples + 2 // one warm-up request plus slack for occasional reconnects
	}

	for range make([]struct{}, attempts) {
		s, err := collectSample(client, cfg.url, cfg.mode == modeCold)
		if err != nil {
			return report{}, err
		}
		if cfg.mode == modeWarmH2 && !s.ReusedConnection {
			continue
		}
		samples = append(samples, s)
		statusCodes[s.StatusCode]++
		if s.ReusedConnection {
			reusedSamples++
		}
		if len(samples) == cfg.samples {
			return summarize(cfg.url, cfg.mode, samples, reusedSamples, statusCodes), nil
		}
	}

	return report{}, fmt.Errorf("collected %d/%d samples in %q mode", len(samples), cfg.samples, cfg.mode)
}

func collectSample(client *http.Client, url string, forceClose bool) (sample, error) {
	var (
		start            time.Time
		gotConnAt        time.Time
		gotTLSHandshake  time.Time
		wroteRequestAt   time.Time
		gotFirstByteAt   time.Time
		reusedConnection bool
	)

	trace := &httptrace.ClientTrace{
		ConnectStart: func(_, _ string) {
			start = time.Now()
		},
		ConnectDone: func(_, _ string, _ error) {
			gotConnAt = time.Now()
		},
		GotConn: func(info httptrace.GotConnInfo) {
			reusedConnection = info.Reused
			if start.IsZero() {
				start = time.Now()
			}
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			gotTLSHandshake = time.Now()
		},
		WroteRequest: func(httptrace.WroteRequestInfo) {
			wroteRequestAt = time.Now()
		},
		GotFirstResponseByte: func() {
			gotFirstByteAt = time.Now()
		},
	}

	ctx := httptrace.WithClientTrace(context.Background(), trace)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return sample{}, fmt.Errorf("build request: %w", err)
	}
	request.Close = forceClose

	if start.IsZero() {
		start = time.Now()
	}
	response, err := client.Do(request)
	if err != nil {
		return sample{}, fmt.Errorf("request %q: %w", url, err)
	}
	defer response.Body.Close()

	_, _ = io.Copy(io.Discard, response.Body)
	end := time.Now()

	if gotConnAt.IsZero() {
		gotConnAt = start
	}
	if gotTLSHandshake.IsZero() {
		gotTLSHandshake = gotConnAt
	}
	if gotFirstByteAt.IsZero() {
		gotFirstByteAt = end
	}

	if wroteRequestAt.IsZero() {
		wroteRequestAt = gotConnAt
	}

	return sample{
		StatusCode:       response.StatusCode,
		Connect:          gotConnAt.Sub(start),
		TLS:              gotTLSHandshake.Sub(start),
		TTFB:             gotFirstByteAt.Sub(start),
		WriteToFirstByte: gotFirstByteAt.Sub(wroteRequestAt),
		Total:            end.Sub(start),
		ReusedConnection: reusedConnection,
	}, nil
}

func summarize(url string, mode string, samples []sample, reusedSamples int, statusCodes map[int]int) report {
	return report{
		URL:                   url,
		Mode:                  mode,
		Samples:               len(samples),
		ReusedSamples:         reusedSamples,
		StatusCodes:           statusCodes,
		AvgConnectMS:          averageDurationMS(samples, func(s sample) time.Duration { return s.Connect }),
		AvgTLSMS:              averageDurationMS(samples, func(s sample) time.Duration { return s.TLS }),
		AvgTTFBMS:             averageDurationMS(samples, func(s sample) time.Duration { return s.TTFB }),
		AvgWriteToFirstByteMS: averageDurationMS(samples, func(s sample) time.Duration { return s.WriteToFirstByte }),
		AvgTotalMS:            averageDurationMS(samples, func(s sample) time.Duration { return s.Total }),
	}
}

func averageDurationMS(samples []sample, get func(sample) time.Duration) float64 {
	if len(samples) == 0 {
		return 0
	}
	var total time.Duration
	for _, sample := range samples {
		total += get(sample)
	}
	return float64(total) / float64(len(samples)) / float64(time.Millisecond)
}

func writeTextReport(w io.Writer, report report) {
	fmt.Fprintf(w, "url=%s\n", report.URL)
	fmt.Fprintf(w, "mode=%s\n", report.Mode)
	fmt.Fprintf(w, "samples=%d\n", report.Samples)
	fmt.Fprintf(w, "reused_samples=%d\n", report.ReusedSamples)
	for _, code := range sortedStatusCodes(report.StatusCodes) {
		fmt.Fprintf(w, "http_code[%d]=%d\n", code, report.StatusCodes[code])
	}
	fmt.Fprintf(
		w,
		"avg_connect=%.1fms avg_tls=%.1fms avg_ttfb=%.1fms avg_write_to_first_byte=%.1fms avg_total=%.1fms\n",
		report.AvgConnectMS,
		report.AvgTLSMS,
		report.AvgTTFBMS,
		report.AvgWriteToFirstByteMS,
		report.AvgTotalMS,
	)
}

func sortedStatusCodes(codes map[int]int) []int {
	keys := make([]int, 0, len(codes))
	for code := range codes {
		keys = append(keys, code)
	}
	sort.Ints(keys)
	return keys
}
