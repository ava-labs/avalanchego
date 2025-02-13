// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestL1ValidatorRegistration(t *testing.T) {
	booleans := []bool{true, false}
	for _, registered := range booleans {
		t.Run(strconv.FormatBool(registered), func(t *testing.T) {
			require := require.New(t)

			msg, err := NewL1ValidatorRegistration(
				ids.GenerateTestID(),
				registered,
			)
			require.NoError(err)

			parsed, err := ParseL1ValidatorRegistration(msg.Bytes())
			require.NoError(err)
			require.Equal(msg, parsed)
		})
	}
}
