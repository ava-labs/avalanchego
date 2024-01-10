// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"errors"
	"fmt"
	"strings"
)

const (
	GRPC ExporterType = iota + 1
	HTTP
)

var errUnknownExporterType = errors.New("unknown exporter type")

func ExporterTypeFromString(exporterTypeStr string) (ExporterType, error) {
	switch strings.ToLower(exporterTypeStr) {
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

func (t ExporterType) String() string {
	switch t {
	case GRPC:
		return "grpc"
	case HTTP:
		return "http"
	default:
		return "unknown"
	}
}
