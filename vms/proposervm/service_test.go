// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
)

func TestServiceGetProposedHeight(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
		graniteTime    = activationTime
	)

	_, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, graniteTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	proposerAPI := &ProposerAPI{vm: proVM}
	proVM.lastAcceptedHeight = 42

	reply := api.GetHeightResponse{}
	require.NoError(proposerAPI.GetProposedHeight(&http.Request{}, nil, &reply))

	require.Equal(proVM.lastAcceptedHeight, uint64(reply.Height))
}
