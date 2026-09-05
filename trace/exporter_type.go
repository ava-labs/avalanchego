// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	Disabled ExporterType = iota
	GRPC
	HTTP
)

const (
	disabledStr = "disabled"
	grpcStr     = "grpc"
	httpStr     = "http"
)

// Standard OTel autoconfiguration variables, from
// https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/.
const (
	tracesExporterKey     = "OTEL_TRACES_EXPORTER"
	otlpProtocolKey       = "OTEL_EXPORTER_OTLP_PROTOCOL"
	otlpTracesProtocolKey = "OTEL_EXPORTER_OTLP_TRACES_PROTOCOL"
)

var (
	errUnknownExporterType       = errors.New("unknown exporter type")
	errMissingQuotes             = errors.New("first and last characters should be quotes")
	errUnsupportedTracesExporter = errors.New(`unsupported ` + tracesExporterKey + ` (only "otlp" and "none" are supported)`)
	errUnsupportedOTLPProtocol   = errors.New(`unsupported OTLP protocol (only "grpc" and "http/protobuf" are supported)`)
)

// ExporterTypeFromEnv returns the [ExporterType] configured by the standard
// OTel [tracesExporterKey] variable, resolving the OTLP transport from
// [otlpTracesProtocolKey] or [otlpProtocolKey]. It returns [Disabled] when
// [tracesExporterKey] is unset, keeping tracing opt-in.
func ExporterTypeFromEnv() (ExporterType, error) {
	exporter, ok := os.LookupEnv(tracesExporterKey)
	if !ok {
		return Disabled, nil
	}
	switch strings.ToLower(strings.TrimSpace(exporter)) {
	case "none":
		return Disabled, nil
	case "otlp":
	default:
		return 0, fmt.Errorf("%w: %q", errUnsupportedTracesExporter, exporter)
	}

	protocol := os.Getenv(otlpTracesProtocolKey)
	if protocol == "" {
		protocol = os.Getenv(otlpProtocolKey)
	}
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case grpcStr:
		return GRPC, nil
	case "", "http/protobuf":
		// http/protobuf is the default protocol recommended by the OTel spec.
		return HTTP, nil
	default:
		return 0, fmt.Errorf("%w: %q", errUnsupportedOTLPProtocol, protocol)
	}
}

func ExporterTypeFromString(exporterTypeStr string) (ExporterType, error) {
	switch strings.ToLower(exporterTypeStr) {
	case disabledStr:
		return Disabled, nil
	case grpcStr:
		return GRPC, nil
	case httpStr:
		return HTTP, nil
	default:
		return 0, fmt.Errorf("%w: %q", errUnknownExporterType, exporterTypeStr)
	}
}

type ExporterType byte

func (t ExporterType) MarshalJSON() ([]byte, error) {
	str, ok := t.toString()
	if !ok {
		return nil, fmt.Errorf("%w: %d", errUnknownExporterType, t)
	}
	return []byte(`"` + str + `"`), nil
}

func (t *ExporterType) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == "null" { // If "null", do nothing
		return nil
	}
	if len(str) < 2 {
		return errMissingQuotes
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}

	exporterType, err := ExporterTypeFromString(str[1:lastIndex])
	if err != nil {
		return err
	}
	*t = exporterType
	return nil
}

func (t ExporterType) String() string {
	str, _ := t.toString()
	return str
}

func (t ExporterType) toString() (string, bool) {
	switch t {
	case Disabled:
		return disabledStr, true
	case GRPC:
		return grpcStr, true
	case HTTP:
		return httpStr, true
	default:
		return "unknown", false
	}
}
