// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSubnetValidatorRegistration(t *testing.T) {
	statuses := []RegistrationStatus{
		CurrentlyValidating,
		NotCurrentlyValidating,
		WillNeverValidate,
	}
	for _, status := range statuses {
		t.Run(status.String(), func(t *testing.T) {
			require := require.New(t)

			msg, err := NewSubnetValidatorRegistration(
				ids.GenerateTestID(),
				status,
			)
			require.NoError(err)
			require.NoError(msg.Verify())

			parsed, err := ParseSubnetValidatorRegistration(msg.Bytes())
			require.NoError(err)
			require.Equal(msg, parsed)

			jsonBytes, err := json.MarshalIndent(msg, "", "\t")
			require.NoError(err)

			var unmarshaledMsg SubnetValidatorRegistration
			require.NoError(json.Unmarshal(jsonBytes, &unmarshaledMsg))
			require.NoError(initialize(&unmarshaledMsg))
			require.Equal(msg, &unmarshaledMsg)
		})
	}
}
