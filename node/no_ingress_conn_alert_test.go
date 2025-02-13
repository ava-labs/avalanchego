// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

type mockBootstrappingTracker struct {
	res []ids.ID
}

func (m *mockBootstrappingTracker) Bootstrapping() []ids.ID {
	return m.res
}

type mockValidatorRetriever struct {
	result bool
}

func (m *mockValidatorRetriever) GetValidator(ids.ID, ids.NodeID) (*validators.Validator, bool) {
	return nil, m.result
}

type mockIngressConnectionCounter struct {
	res int
}

func (m *mockIngressConnectionCounter) IngressConnCount() int {
	return m.res
}

func TestNoIngressConnAlertHealthCheck(t *testing.T) {
	start := time.Now()

	for _, testCase := range []struct {
		name                   string
		bootstrappingResult    []ids.ID
		getValidatorResult     bool
		ingressConnCountResult []int
		now                    []func() time.Time
		expectedErr            []error
		expectedResult         []interface{}
	}{
		{
			name:                   "still bootstrapping",
			bootstrappingResult:    []ids.ID{{}},
			now:                    []func() time.Time{time.Now},
			ingressConnCountResult: []int{0},
			expectedErr:            []error{nil},
			expectedResult:         []interface{}{nil},
		},
		{
			name:                   "some are connected to us",
			now:                    []func() time.Time{time.Now},
			ingressConnCountResult: []int{42},
			expectedErr:            []error{nil},
			expectedResult:         []interface{}{nil},
		},
		{
			name:                   "first check",
			now:                    []func() time.Time{time.Now},
			ingressConnCountResult: []int{0},
			expectedErr:            []error{nil},
			expectedResult:         []interface{}{nil},
		},
		{
			name: "validator but not enough time has passed between checks",
			now: []func() time.Time{
				func() time.Time { return start },
				func() time.Time { return start.Add(time.Minute) },
			},
			ingressConnCountResult: []int{0, 0},
			expectedErr:            []error{nil, nil},
			expectedResult:         []interface{}{nil, nil},
			getValidatorResult:     true,
		},
		{
			name: "enough time has passed between checks but not a validator",
			now: []func() time.Time{
				func() time.Time { return start },
				func() time.Time { return start.Add(time.Hour) },
			},
			ingressConnCountResult: []int{0, 0},
			expectedErr:            []error{nil, nil},
			expectedResult:         []interface{}{nil, map[string]interface{}{"ingressConnectionCount": 0, "primary network validator": false}},
		},
		{
			name: "enough time has passed between checks and also a validator",
			now: []func() time.Time{
				func() time.Time { return start },
				func() time.Time { return start.Add(time.Hour) },
			},
			ingressConnCountResult: []int{0, 0},
			expectedErr:            []error{nil, ErrNoIngressConnections},
			expectedResult:         []interface{}{nil, map[string]interface{}{"ingressConnectionCount": 0, "primary network validator": true}},
			getValidatorResult:     true,
		},
		{
			name: "recovery",
			now: []func() time.Time{
				func() time.Time { return start },
				func() time.Time { return start.Add(time.Hour) },
				func() time.Time { return start.Add(time.Hour).Add(time.Minute) },
			},
			ingressConnCountResult: []int{0, 0, 42},
			expectedErr:            []error{nil, ErrNoIngressConnections, nil},
			expectedResult: []interface{}{
				nil,
				map[string]interface{}{"ingressConnectionCount": 0, "primary network validator": true},
				map[string]interface{}{"ingressConnectionCount": 42, "primary network validator": true},
			},
			getValidatorResult: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.NotEmpty(t, testCase.now)
			require.Equal(t, len(testCase.now), len(testCase.expectedErr))
			require.Equal(t, len(testCase.now), len(testCase.expectedResult))
			require.Equal(t, len(testCase.now), len(testCase.ingressConnCountResult))

			nica := &noIngressConnAlert{
				selfID:           ids.EmptyNodeID,
				minCheckInterval: 20 * time.Minute,
				bootstrapping:    &mockBootstrappingTracker{res: testCase.bootstrappingResult},
				validators:       &mockValidatorRetriever{result: testCase.getValidatorResult},
			}

			for i, now := range testCase.now {
				nica.now = now
				nica.ingressConnections = &mockIngressConnectionCounter{res: testCase.ingressConnCountResult[i]}
				result, err := nica.HealthCheck(context.Background())
				require.Equal(t, testCase.expectedErr[i], err, i)
				require.Equal(t, testCase.expectedResult[i], result, i)
			}
		})
	}
}
