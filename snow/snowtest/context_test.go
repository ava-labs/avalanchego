// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowtest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/trace"
)

func TestContextTracerDefaultsToNoop(t *testing.T) {
	ctx := Context(t, ids.GenerateTestID())
	require.Equal(t, trace.Noop, ctx.Tracer, "snowtest.Context() must populate Tracer so VMs can use it without nil checks")
}
