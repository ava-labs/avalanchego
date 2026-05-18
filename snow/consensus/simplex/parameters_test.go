// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParametersVerify(t *testing.T) {
	validValidators := []ValidatorInfo{{}}

	tests := []struct {
		name          string
		params        Parameters
		expectedError error
	}{
		{
			name: "valid",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  validValidators,
			},
			expectedError: nil,
		},
		{
			name: "zero MaxNetworkDelay",
			params: Parameters{
				MaxNetworkDelay:    0,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  validValidators,
			},
			expectedError: ErrInvalidParameters,
		},
		{
			name: "zero MaxRebroadcastWait",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: 0,
				InitialValidators:  validValidators,
			},
			expectedError: ErrInvalidParameters,
		},
		{
			name: "empty InitialValidators",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  []ValidatorInfo{},
			},
			expectedError: ErrInvalidParameters,
		},
		{
			name: "nil InitialValidators",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  nil,
			},
			expectedError: ErrInvalidParameters,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.params.Verify()
			require.ErrorIs(t, err, test.expectedError)
		})
	}
}
