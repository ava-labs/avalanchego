// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import (
	"context"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

var _ oracle.SidecarClient = (*MockSidecarClient)(nil)

// MockSidecarClient is a test double for SidecarClient. Set VerifyF to
// control behavior per call. If VerifyF is nil, Verify returns nil (accept
// everything). Calls are recorded in Calls for assertion.
//
// MockSidecarClient is not safe for concurrent use; it is intended for
// single-threaded tests only.
type MockSidecarClient struct {
	// VerifyF is called by Verify if non-nil.
	VerifyF func(ctx context.Context, event *oracle.OracleEvent) error
	// Calls records every event passed to Verify, in order.
	Calls []*oracle.OracleEvent
}

func (m *MockSidecarClient) Verify(ctx context.Context, event *oracle.OracleEvent) error {
	m.Calls = append(m.Calls, event)
	if m.VerifyF != nil {
		return m.VerifyF(ctx, event)
	}
	return nil
}
