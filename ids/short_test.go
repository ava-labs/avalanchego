// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestShortIDMapMarshalling guards against ShortID.UnmarshalText forwarding
// to UnmarshalJSON, which requires quoted input. encoding/json calls
// UnmarshalText with the *unquoted* form of a string when decoding a JSON
// object key, so a ShortID-keyed map could previously be marshalled but never
// unmarshalled back.
func TestShortIDMapMarshalling(t *testing.T) {
	require := require.New(t)

	originalMap := map[ShortID]int{
		{'e', 'v', 'a', ' ', 'l', 'a', 'b', 's'}: 1,
		{'a', 'v', 'a', ' ', 'l', 'a', 'b', 's'}: 2,
	}
	mapJSON, err := json.Marshal(originalMap)
	require.NoError(err)

	var unmarshalledMap map[ShortID]int
	require.NoError(json.Unmarshal(mapJSON, &unmarshalledMap))

	require.Equal(originalMap, unmarshalledMap)
}
