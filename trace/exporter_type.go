// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"errors"
	"fmt"
	"strings"
)

const (
	Disabled ExporterType = iota
	GRPC
	HTTP
)

var (
	errUnknownExporterType = errors.New("unknown exporter type")
	errMissingQuotes       = errors.New("first and last characters should be quotes")
)

func ExporterTypeFromString(exporterTypeStr string) (ExporterType, error) {
	switch strings.ToLower(exporterTypeStr) {
	case "disabled":
		return Disabled, nil
	case "grpc":
		return GRPC, nil
	case "http":
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
		return "disabled", true
	case GRPC:
		return "grpc", true
	case HTTP:
		return "http", true
	default:
		return "unknown", false
	}
}
