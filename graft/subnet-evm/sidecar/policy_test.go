// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sidecar

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/warp/external"
)

// stubRule is a hand-rolled ValidationRule for testing.
type stubRule struct {
	pass bool
	err  error
}

func (s *stubRule) Validate(_ context.Context, _ *external.ExternalEvent) (bool, error) {
	return s.pass, s.err
}

var (
	pass          = &stubRule{pass: true}
	fail          = &stubRule{pass: false}
	errRPCTimeout = errors.New("rpc timeout")
	infraErr      = &stubRule{err: errRPCTimeout}
)

var testEvent = &external.ExternalEvent{}

func TestPolicy_NoRules(t *testing.T) {
	r := &ExternalInteropValidationRule{}
	require.NoError(t, r.Verify(t.Context(), testEvent))
}

func TestPolicy_RequiredAllPass(t *testing.T) {
	r := &ExternalInteropValidationRule{
		Required: []ValidationRule{pass, pass},
	}
	require.NoError(t, r.Verify(t.Context(), testEvent))
}

func TestPolicy_RequiredOneFails(t *testing.T) {
	r := &ExternalInteropValidationRule{
		Required: []ValidationRule{pass, fail, pass},
	}
	err := r.Verify(t.Context(), testEvent)
	require.ErrorIs(t, err, ErrRequiredRuleFailed)
}

func TestPolicy_RequiredInfraErrorFails(t *testing.T) {
	r := &ExternalInteropValidationRule{
		Required: []ValidationRule{pass, infraErr},
	}
	err := r.Verify(t.Context(), testEvent)
	require.ErrorIs(t, err, errRPCTimeout)
}

func TestPolicy_RequiredEarlyExitsBeforeQuorum(t *testing.T) {
	// Quorum would pass but required fails first — Quorum rules are never called.
	quorumCalled := false
	quorumRule := &callTrackingRule{inner: pass, called: &quorumCalled}
	r := &ExternalInteropValidationRule{
		Required:  []ValidationRule{fail},
		Quorum:    []ValidationRule{quorumRule},
		MinQuorum: 1,
	}
	err := r.Verify(t.Context(), testEvent)
	require.ErrorIs(t, err, ErrRequiredRuleFailed)
	require.False(t, quorumCalled)
}

func TestPolicy_QuorumExactlyAtThreshold(t *testing.T) {
	r := &ExternalInteropValidationRule{
		Quorum:    []ValidationRule{pass, fail, pass},
		MinQuorum: 2,
	}
	require.NoError(t, r.Verify(t.Context(), testEvent))
}

func TestPolicy_QuorumBelowThreshold(t *testing.T) {
	r := &ExternalInteropValidationRule{
		Quorum:    []ValidationRule{pass, fail, fail},
		MinQuorum: 2,
	}
	err := r.Verify(t.Context(), testEvent)
	require.ErrorIs(t, err, ErrQuorumNotMet)
}

func TestPolicy_QuorumInfraErrorAbstains(t *testing.T) {
	// One pass, one infra error (abstain), need 1 — should pass.
	r := &ExternalInteropValidationRule{
		Quorum:    []ValidationRule{pass, infraErr},
		MinQuorum: 1,
	}
	require.NoError(t, r.Verify(t.Context(), testEvent))
}

func TestPolicy_QuorumAllInfraErrors(t *testing.T) {
	r := &ExternalInteropValidationRule{
		Quorum:    []ValidationRule{infraErr, infraErr},
		MinQuorum: 1,
	}
	err := r.Verify(t.Context(), testEvent)
	require.ErrorIs(t, err, ErrQuorumNotMet)
}

func TestPolicy_RequiredPassQuorumFails(t *testing.T) {
	r := &ExternalInteropValidationRule{
		Required:  []ValidationRule{pass},
		Quorum:    []ValidationRule{fail, fail},
		MinQuorum: 1,
	}
	err := r.Verify(t.Context(), testEvent)
	require.ErrorIs(t, err, ErrQuorumNotMet)
}

// callTrackingRule wraps a ValidationRule and records whether Validate was called.
type callTrackingRule struct {
	inner  ValidationRule
	called *bool
}

func (c *callTrackingRule) Validate(ctx context.Context, event *external.ExternalEvent) (bool, error) {
	*c.called = true
	return c.inner.Validate(ctx, event)
}
