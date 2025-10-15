// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
	"testing"
	"fmt"
)

func TestFoo(t *testing.T) {
	v := validator{
		UpDuration:    123,
		LastUpdated:   123,
		NodeID:        ids.NodeID{1, 2, 3},
		Weight:        123,
		StartTime:     123,
		IsActive:      true,
		IsL1Validator: true,
		validationID:  ids.ID{1, 2, 3},
	}

	bytes, err := codecManager.Marshal(0, v)
	require.NoError(t, err)

	fmt.Printf("%x\n", bytes)
}
