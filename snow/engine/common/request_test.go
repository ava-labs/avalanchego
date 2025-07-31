// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestRequestJSONMarshal(t *testing.T) {
	requestMap := map[Request]ids.ID{
		{
			NodeID:    ids.GenerateTestNodeID(),
			RequestID: 12345,
		}: ids.GenerateTestID(),
	}
	_, err := json.Marshal(requestMap)
	require.NoError(t, err)
}
