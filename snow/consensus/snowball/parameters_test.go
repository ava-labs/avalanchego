// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParametersVerify(t *testing.T) {
	tests := []struct {
		name          string
		params        Parameters
		expectedError error
	}{
		{
			name: "valid",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				BetaVirtuous:          1,
				BetaRogue:             1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedError: nil,
		},
		{
			name: "invalid K",
			params: Parameters{
				K:                     0,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				BetaVirtuous:          1,
				BetaRogue:             1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedError: ErrParametersInvalid,
		},
		{
			name: "invalid AlphaPreference 1",
			params: Parameters{
				K:                     2,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				BetaVirtuous:          1,
				BetaRogue:             1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedError: ErrParametersInvalid,
		},
		{
			name: "invalid AlphaPreference 0",
			params: Parameters{
				K:                     1,
				AlphaPreference:       0,
				AlphaConfidence:       1,
				BetaVirtuous:          1,
				BetaRogue:             1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedError: ErrParametersInvalid,
		},
		{
			name: "invalid AlphaConfidence",
			params: Parameters{
				K:                     3,
				AlphaPreference:       3,
				AlphaConfidence:       2,
				BetaVirtuous:          1,
				BetaRogue:             1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedError: ErrParametersInvalid,
		},
		{
			name: "invalid BetaVirtuous",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				BetaVirtuous:          0,
				BetaRogue:             1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedError: ErrParametersInvalid,
		},
		{
			name: "first half fun BetaRogue",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				BetaVirtuous:          28,
				BetaRogue:             30,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedError: nil,
		},
		{
			name: "second half fun BetaRogue",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				BetaVirtuous:          2,
				BetaRogue:             3,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedError: nil,
		},
		{
			name: "fun invalid BetaRogue",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				BetaVirtuous:          28,
				BetaRogue:             3,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedError: ErrParametersInvalid,
		},
		{
			name: "invalid BetaRogue",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				BetaVirtuous:          2,
				BetaRogue:             1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedError: ErrParametersInvalid,
		},
		{
			name: "too few ConcurrentRepolls",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				BetaVirtuous:          1,
				BetaRogue:             1,
				ConcurrentRepolls:     0,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedError: ErrParametersInvalid,
		},
		{
			name: "too many ConcurrentRepolls",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				BetaVirtuous:          1,
				BetaRogue:             1,
				ConcurrentRepolls:     2,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedError: ErrParametersInvalid,
		},
		{
			name: "invalid OptimalProcessing",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				BetaVirtuous:          1,
				BetaRogue:             1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     0,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedError: ErrParametersInvalid,
		},
		{
			name: "invalid MaxOutstandingItems",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				BetaVirtuous:          1,
				BetaRogue:             1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   0,
				MaxItemProcessingTime: 1,
			},
			expectedError: ErrParametersInvalid,
		},
		{
			name: "invalid MaxItemProcessingTime",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				BetaVirtuous:          1,
				BetaRogue:             1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 0,
			},
			expectedError: ErrParametersInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.params.Verify()
			require.ErrorIs(t, err, test.expectedError)
		})
	}
}

func TestParametersMinPercentConnectedHealthy(t *testing.T) {
	tests := []struct {
		name                        string
		params                      Parameters
		expectedMinPercentConnected float64
	}{
		{
			name:                        "default",
			params:                      DefaultParameters,
			expectedMinPercentConnected: 0.8,
		},
		{
			name: "custom",
			params: Parameters{
				K:               5,
				AlphaConfidence: 4,
			},
			expectedMinPercentConnected: 0.84,
		},
		{
			name: "custom",
			params: Parameters{
				K:               1001,
				AlphaConfidence: 501,
			},
			expectedMinPercentConnected: 0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minStake := tt.params.MinPercentConnectedHealthy()
			require.InEpsilon(t, tt.expectedMinPercentConnected, minStake, .001)
		})
	}
}
