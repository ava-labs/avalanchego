// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	// Alloy configuration
	alloyCmd               = "alloy"
	alloyHTTPListenAddress = "127.0.0.1:12345"
	alloyReadinessURL      = "http://" + alloyHTTPListenAddress + "/-/ready"
	alloyConfigFilename    = "config.alloy"
	alloyOTLPListenAddress = "127.0.0.1:4317"
)

var (
	errTempoPushURLHasPath = errors.New("TEMPO_PUSH_URL must be the Tempo base URL without a /v1/traces suffix")
	errTempoPushURLScheme  = errors.New("TEMPO_PUSH_URL must use the http or https scheme")
)

// StartAlloy ensures alloy is running to forward traces from local nodes to Tempo.
func StartAlloy(ctx context.Context, log logging.Logger) error {
	if _, ok := ctx.Deadline(); !ok {
		return stacktrace.New("unable to start alloy with a context without a deadline")
	}
	if err := startAlloy(ctx, log); err != nil {
		return stacktrace.Wrap(err)
	}
	if err := waitForReadiness(ctx, log, alloyCmd, alloyReadinessURL); err != nil {
		return stacktrace.Wrap(err)
	}
	log.Info("To stop: tmpnetctl stop-traces-collector")
	return nil
}

// StopTracesCollector ensures alloy is not running.
func StopTracesCollector(ctx context.Context, log logging.Logger) error {
	return stopCollector(ctx, log, alloyCmd)
}

// alloyRunning returns true if a live process is recorded in alloy's PID file.
func alloyRunning() (bool, error) {
	workingDir, err := getWorkingDir(alloyCmd)
	if err != nil {
		return false, stacktrace.Wrap(err)
	}
	process, err := processFromPIDFile(alloyCmd, getPIDPath(workingDir))
	if err != nil {
		return false, stacktrace.Wrap(err)
	}
	return process != nil, nil
}

// tracingFlagsIfAlloyRunning returns the node tracing flags if alloy is
// running, or nil otherwise. Failures are logged rather than returned so
// that a collector problem can never prevent a network from starting.
func tracingFlagsIfAlloyRunning(log logging.Logger, labels map[string]string) FlagsMap {
	running, err := alloyRunning()
	if err != nil {
		log.Warn("failed to determine whether the alloy traces collector is running; nodes will not export traces",
			zap.Error(err),
		)
		return nil
	}
	if !running {
		return nil
	}
	flags, err := tracingFlagsForLabels(labels)
	if err != nil {
		log.Warn("failed to compute tracing flags; nodes will not export traces",
			zap.Error(err),
		)
		return nil
	}
	return flags
}

func startAlloy(ctx context.Context, log logging.Logger) error {
	cfg, err := getCollectorConfigForPush(alloyCmd)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	if err := validateTempoPushURL(cfg.url); err != nil {
		return stacktrace.Wrap(err)
	}
	args := "run --server.http.listen-addr=" + alloyHTTPListenAddress + " --storage.path=./data " + alloyConfigFilename
	return stacktrace.Wrap(startCollector(ctx, log, alloyCmd, args, alloyConfigFilename, alloyConfig(cfg)))
}

// validateTempoPushURL rejects URLs the otlphttp exporter would mishandle: it
// appends /v1/traces itself, so the URL must be the bare Tempo base URL.
func validateTempoPushURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return stacktrace.Errorf("failed to parse TEMPO_PUSH_URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return stacktrace.Errorf("%w: %s", errTempoPushURLScheme, raw)
	}
	if strings.HasSuffix(strings.TrimSuffix(u.Path, "/"), "/v1/traces") {
		return stacktrace.Errorf("%w: %s", errTempoPushURLHasPath, raw)
	}
	return nil
}

// alloyConfig returns an alloy config that receives OTLP/gRPC from local nodes
// and forwards it to Tempo.
func alloyConfig(c collectorConfig) string {
	return fmt.Sprintf(`otelcol.receiver.otlp "nodes" {
  grpc {
    endpoint = "%s"
  }
  output {
    traces = [otelcol.processor.batch.default.input]
  }
}

otelcol.processor.batch "default" {
  output {
    traces = [otelcol.exporter.otlphttp.tempo.input]
  }
}

otelcol.auth.basic "tempo" {
  username = "%s"
  password = "%s"
}

otelcol.exporter.otlphttp "tempo" {
  client {
    endpoint = "%s"
    auth     = otelcol.auth.basic.tempo.handler
  }
}
`, alloyOTLPListenAddress, c.username, c.password, c.url)
}

// tracingFlagsForLabels returns the node flags that export traces to alloy,
// tagging them with the monitoring labels (minus the per-node instance label).
func tracingFlagsForLabels(labels map[string]string) (FlagsMap, error) {
	attrs := make(map[string]string, len(labels))
	for k, v := range labels {
		attrs[k] = v
	}
	delete(attrs, "instance")
	encoded, err := json.Marshal(attrs)
	if err != nil {
		return nil, stacktrace.Errorf("failed to marshal tracing resource attributes: %w", err)
	}
	return FlagsMap{
		config.TracingExporterTypeKey:       "grpc",
		config.TracingEndpointKey:           alloyOTLPListenAddress,
		config.TracingInsecureKey:           "true",
		config.TracingSampleRateKey:         "1",
		config.TracingResourceAttributesKey: string(encoded),
	}, nil
}
