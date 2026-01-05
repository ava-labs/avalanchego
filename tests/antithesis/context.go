// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package antithesis

import (
	"context"
	"fmt"
	"maps"

	"github.com/antithesishq/antithesis-sdk-go/assert"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// NewInstrumentedTestContext returns a test context that makes antithesis SDK assertions.
func NewInstrumentedTestContext(log logging.Logger) *tests.SimpleTestContext {
	return NewInstrumentedTestContextWithArgs(context.Background(), log, nil)
}

// NewInstrumentedTestContextWithArgs returns a test context that makes antithesis SDK assertions.
func NewInstrumentedTestContextWithArgs(
	ctx context.Context,
	log logging.Logger,
	details map[string]any,
) *tests.SimpleTestContext {
	return tests.NewTestContextWithArgs(
		ctx,
		log,
		func(format string, args ...any) {
			assert.Unreachable(fmt.Sprintf("Assertion failure: "+format, args...), details)
		},
		func(r any) {
			detailsClone := maps.Clone(details)
			detailsClone["panic"] = r
			assert.Unreachable("unexpected panic", detailsClone)
		},
	)
}
