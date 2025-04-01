// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"errors"
	"fmt"
	"strings"
)

const (
	NoOp ExporterType = iota
	GRPC
	HTTP
)

var (
	errUnknownExporterType = errors.New("unknown exporter type")
	errInvalidFormat       = errors.New("invalid format")
)

func ExporterTypeFromString(exporterTypeStr string) (ExporterType, error) {
	switch strings.ToLower(exporterTypeStr) {
	case NoOp.String():
		return 0, nil
	case "null":
		return 0, nil
	case GRPC.String():
		return GRPC, nil
	case HTTP.String():
		return HTTP, nil
	default:
		return 0, fmt.Errorf("%w: %q", errUnknownExporterType, exporterTypeStr)
	}
}

type ExporterType byte

func (t ExporterType) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.String() + `"`), nil
}

func (t *ExporterType) UnmarshalJSON(b []byte) error {
	exporterTypeStr, err := stripQuotes(string(b))
	if err != nil {
		return err
	}
	exporterType, err := ExporterTypeFromString(exporterTypeStr)
	if err != nil {
		return err
	}
	*t = exporterType
	return nil
}

func (t ExporterType) String() string {
	switch t {
	case NoOp:
		return ""
	case GRPC:
		return "grpc"
	case HTTP:
		return "http"
	default:
		return "unknown"
	}
}

func stripQuotes(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", errInvalidFormat
	}
	return s[1 : len(s)-1], nil
}
