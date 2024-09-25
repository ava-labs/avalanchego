// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSubnetValidatorRegistration(t *testing.T) {
	booleans := []bool{true, false}
	for _, registered := range booleans {
		t.Run(strconv.FormatBool(registered), func(t *testing.T) {
			require := require.New(t)

			msg, err := NewSubnetValidatorRegistration(
				ids.GenerateTestID(),
				registered,
			)
			require.NoError(err)

			parsed, err := ParseSubnetValidatorRegistration(msg.Bytes())
			require.NoError(err)
			require.Equal(msg, parsed)
		})
	}
}
