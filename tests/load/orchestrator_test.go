// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ Issuer[ids.ID] = (*mockIssuer)(nil)

func TestOrchestratorTPS(t *testing.T) {
	tests := []struct {
		name        string
		serverTPS   uint64
		config      OrchestratorConfig
		expectedErr error
	}{
		{
			name:      "orchestrator achieves max TPS",
			serverTPS: math.MaxUint64,
			config: OrchestratorConfig{
				MaxTPS:           2_000,
				MinTPS:           1_000,
				Step:             1_000,
				TxRateMultiplier: 1.5,
				SustainedTime:    time.Second,
				MaxAttempts:      2,
				Terminate:        true,
			},
		},
		{
			name:      "orchestrator TPS limited by network",
			serverTPS: 1_000,
			config: OrchestratorConfig{
				MaxTPS:           2_000,
				MinTPS:           1_000,
				Step:             1_000,
				TxRateMultiplier: 1.3,
				SustainedTime:    time.Second,
				MaxAttempts:      2,
				Terminate:        true,
			},
			expectedErr: ErrFailedToReachTargetTPS,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			m, err := NewMetrics(prometheus.NewRegistry())
			r.NoError(err)

			tracker := NewTracker[ids.ID](m)

			agents := []Agent[ids.ID]{
				NewAgent(
					&mockIssuer{
						generateTxF: func() (ids.ID, error) {
							return ids.GenerateTestID(), nil
						},
						tracker: tracker,
						maxTxs:  tt.serverTPS,
					},
					&mockListener{},
				),
			}

			orchestrator := NewOrchestrator(
				agents,
				tracker,
				logging.NoLog{},
				tt.config,
			)

			r.ErrorIs(orchestrator.Execute(ctx), tt.expectedErr)

			if tt.expectedErr == nil {
				r.GreaterOrEqual(orchestrator.GetMaxObservedTPS(), tt.config.MaxTPS)
			} else {
				r.Less(orchestrator.GetMaxObservedTPS(), tt.config.MaxTPS)
			}
		})
	}
}

// test that the orchestrator returns early if the txGenerators or the issuers error
func TestOrchestratorExecution(t *testing.T) {
	var (
		errMockTxGenerator = errors.New("mock tx generator error")
		errMockIssuer      = errors.New("mock issuer error")
	)

	m, err := NewMetrics(prometheus.NewRegistry())
	require.NoError(t, err, "creating metrics")
	tracker := NewTracker[ids.ID](m)

	tests := []struct {
		name        string
		agents      []Agent[ids.ID]
		expectedErr error
	}{
		{
			name: "generator error",
			agents: []Agent[ids.ID]{
				NewAgent(
					&mockIssuer{
						generateTxF: func() (ids.ID, error) {
							return ids.Empty, errMockTxGenerator
						},
					},
					&mockListener{},
				),
			},
			expectedErr: errMockTxGenerator,
		},
		{
			name: "issuer error",
			agents: []Agent[ids.ID]{
				NewAgent(
					&mockIssuer{
						generateTxF: func() (ids.ID, error) {
							return ids.GenerateTestID(), nil
						},
						issueTxErr: errMockIssuer,
					},
					&mockListener{},
				),
			},
			expectedErr: errMockIssuer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			orchestrator := NewOrchestrator(
				tt.agents,
				tracker,
				logging.NoLog{},
				NewOrchestratorConfig(),
			)
			r.NoError(err)

			r.ErrorIs(orchestrator.Execute(ctx), tt.expectedErr)
		})
	}
}

type mockIssuer struct {
	generateTxF   func() (ids.ID, error)
	currTxsIssued uint64
	maxTxs        uint64
	tracker       *Tracker[ids.ID]
	issueTxErr    error
}

// GenerateAndIssueTx immediately generates, issues and confirms a tx.
// To simulate TPS, the number of txs IssueTx can issue/confirm is capped by maxTxs
func (m *mockIssuer) GenerateAndIssueTx(_ context.Context) (ids.ID, error) {
	id, err := m.generateTxF()
	if err != nil {
		return id, err
	}

	if m.issueTxErr != nil {
		return ids.ID{}, m.issueTxErr
	}

	if m.currTxsIssued >= m.maxTxs {
		return ids.ID{}, nil
	}

	m.tracker.Issue(id)
	m.tracker.ObserveConfirmed(id)
	m.currTxsIssued++
	return id, nil
}

type mockListener struct{}

func (*mockListener) Listen(context.Context) error {
	return nil
}

func (*mockListener) RegisterIssued(ids.ID) {}

func (*mockListener) IssuingDone() {}
