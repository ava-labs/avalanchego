// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshallHeight(t *testing.T) {
	require := require.New(t)
	h := Height(56)
	bytes, err := h.MarshalJSON()
	require.NoError(err)
	require.Equal(`"56"`, string(bytes))

	h = Height(ProposedHeight)
	bytes, err = h.MarshalJSON()
	require.NoError(err)
	require.Equal(`"proposed"`, string(bytes))
}

func TestUnmarshallHeight(t *testing.T) {
	require := require.New(t)

	var h Height

	require.NoError(h.UnmarshalJSON([]byte("56")))
	require.Equal(Height(56), h)

	marshalledFlagBytes, err := json.Marshal(ProposedHeightFlag)
	require.NoError(err)

	require.NoError(h.UnmarshalJSON(marshalledFlagBytes))
	require.Equal(Height(ProposedHeight), h)
	require.True(h.IsProposed())

	err = h.UnmarshalJSON([]byte("invalid"))
	require.ErrorIs(err, errInvalidHeight)
	require.Equal(Height(0), h)

	err = h.UnmarshalJSON([]byte(`"` + strconv.FormatUint(uint64(ProposedHeight), 10) + `"`))
	require.ErrorIs(err, errInvalidHeight)
	require.Equal(Height(0), h)
}
