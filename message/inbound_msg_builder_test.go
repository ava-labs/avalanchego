// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"
)

func Test_newMsgBuilder(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	mb, err := newMsgBuilder(
		"test",
		prometheus.NewRegistry(),
		10*time.Second,
	)
	require.NoError(err)
	require.NotNil(mb)
}
