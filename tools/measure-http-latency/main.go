package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
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

type config struct {
	url     string
	samples int
	timeout time.Duration
	format  string
}

type sample struct {
	StatusCode int           `json:"statusCode"`
	Connect    time.Duration `json:"-"`
	TLS        time.Duration `json:"-"`
	TTFB       time.Duration `json:"-"`
	Total      time.Duration `json:"-"`
}

type report struct {
	URL          string      `json:"url"`
	Samples      int         `json:"samples"`
	StatusCodes  map[int]int `json:"statusCodes"`
	AvgConnectMS float64     `json:"avgConnectMs"`
	AvgTLSMS     float64     `json:"avgTlsMs"`
	AvgTTFBMS    float64     `json:"avgTtfbMs"`
	AvgTotalMS   float64     `json:"avgTotalMs"`
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

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.url == "" {
		return config{}, fmt.Errorf("--url is required")
	}
	if cfg.samples <= 0 {
		return config{}, fmt.Errorf("--samples must be > 0")
	}
	cfg.format = strings.ToLower(cfg.format)
	if cfg.format != "text" && cfg.format != "json" {
		return config{}, fmt.Errorf("--format must be one of: text, json")
	}
	if cfg.timeout <= 0 {
		return config{}, fmt.Errorf("--timeout must be > 0")
	}
	return cfg, nil
}

func measure(cfg config) (report, error) {
	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		DisableKeepAlives: true,
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
	for range make([]struct{}, cfg.samples) {
		s, err := collectSample(client, cfg.url)
		if err != nil {
			return report{}, err
		}
		samples = append(samples, s)
		statusCodes[s.StatusCode]++
	}

	return summarize(cfg.url, samples, statusCodes), nil
}

func collectSample(client *http.Client, url string) (sample, error) {
	var (
		start           time.Time
		gotConnAt       time.Time
		gotTLSHandshake time.Time
		gotFirstByteAt  time.Time
	)

	trace := &httptrace.ClientTrace{
		ConnectStart: func(_, _ string) {
			start = time.Now()
		},
		ConnectDone: func(_, _ string, _ error) {
			gotConnAt = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			gotTLSHandshake = time.Now()
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
	request.Close = true

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
		gotConnAt = end
	}
	if gotTLSHandshake.IsZero() {
		gotTLSHandshake = gotConnAt
	}
	if gotFirstByteAt.IsZero() {
		gotFirstByteAt = end
	}

	return sample{
		StatusCode: response.StatusCode,
		Connect:    gotConnAt.Sub(start),
		TLS:        gotTLSHandshake.Sub(start),
		TTFB:       gotFirstByteAt.Sub(start),
		Total:      end.Sub(start),
	}, nil
}

func summarize(url string, samples []sample, statusCodes map[int]int) report {
	return report{
		URL:          url,
		Samples:      len(samples),
		StatusCodes:  statusCodes,
		AvgConnectMS: averageDurationMS(samples, func(s sample) time.Duration { return s.Connect }),
		AvgTLSMS:     averageDurationMS(samples, func(s sample) time.Duration { return s.TLS }),
		AvgTTFBMS:    averageDurationMS(samples, func(s sample) time.Duration { return s.TTFB }),
		AvgTotalMS:   averageDurationMS(samples, func(s sample) time.Duration { return s.Total }),
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
	fmt.Fprintf(w, "samples=%d\n", report.Samples)
	for _, code := range sortedStatusCodes(report.StatusCodes) {
		fmt.Fprintf(w, "http_code[%d]=%d\n", code, report.StatusCodes[code])
	}
	fmt.Fprintf(
		w,
		"avg_connect=%.1fms avg_tls=%.1fms avg_ttfb=%.1fms avg_total=%.1fms\n",
		report.AvgConnectMS,
		report.AvgTLSMS,
		report.AvgTTFBMS,
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
