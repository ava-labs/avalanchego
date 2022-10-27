// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import "github.com/ava-labs/avalanchego/trace"

var _ StateSyncer = (*tracedStateSyncer)(nil)

type tracedStateSyncer struct {
	Engine
	stateSyncer StateSyncer
}

func TraceStateSyncer(stateSyncer StateSyncer, tracer trace.Tracer) StateSyncer {
	return &tracedStateSyncer{
		Engine:      TraceEngine(stateSyncer, tracer),
		stateSyncer: stateSyncer,
	}
}

func (e *tracedStateSyncer) IsEnabled() (bool, error) {
	return e.stateSyncer.IsEnabled()
}
