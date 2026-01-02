// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import "go.opentelemetry.io/otel/trace/noop"

var Noop Tracer = noOpTracer{}

// noOpTracer is an implementation of trace.Tracer that does nothing.
type noOpTracer struct {
	noop.Tracer
}

func (noOpTracer) Close() error {
	return nil
}
