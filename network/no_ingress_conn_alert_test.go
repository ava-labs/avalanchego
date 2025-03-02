// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

type fakeValidatorRetriever struct {
	result bool
}

func (m *fakeValidatorRetriever) GetValidator(ids.ID, ids.NodeID) (*validators.Validator, bool) {
	return nil, m.result
}

type fakeIngressConnectionCounter struct {
	res int
}

func (m *fakeIngressConnectionCounter) IngressConnCount() int {
	return m.res
}

func TestNoIngressConnAlertHealthCheck(t *testing.T) {
	for _, testCase := range []struct {
		name                   string
		getValidatorResult     bool
		ingressConnCountResult int
		expectedErr            error
		expectedResult         interface{}
	}{
		{
			name:           "not a validator of a primary network",
			expectedResult: map[string]interface{}{"ingressConnectionCount": 0, "primary network validator": false},
		},
		{
			name:               "a validator of the primary network",
			getValidatorResult: true,
			expectedResult: map[string]interface{}{
				"ingressConnectionCount": 0, "primary network validator": true,
			},
			expectedErr: ErrNoIngressConnections,
		},
		{
			name:                   "a validator with ingress connections",
			expectedResult:         map[string]interface{}{"ingressConnectionCount": 42, "primary network validator": true},
			expectedErr:            nil,
			ingressConnCountResult: 42,
			getValidatorResult:     true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			nica := &noIngressConnAlert{
				selfID:             ids.EmptyNodeID,
				validators:         &fakeValidatorRetriever{result: testCase.getValidatorResult},
				ingressConnections: &fakeIngressConnectionCounter{res: testCase.ingressConnCountResult},
			}

			result, err := nica.checkHealth()
			require.Equal(t, testCase.expectedErr, err)
			require.Equal(t, testCase.expectedResult, result)
		})
	}
}
