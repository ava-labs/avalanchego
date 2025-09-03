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

func TestGetProposedHeight(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)

	_, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	service := &ProposerAPI{vm: proVM}

	reply := api.GetHeightResponse{}
	require.NoError(service.GetProposedHeight(&http.Request{}, nil, &reply))

	minHeight, err := service.vm.ctx.ValidatorState.GetMinimumHeight(context.Background())
	require.NoError(err)
	require.Equal(minHeight, uint64(reply.Height))
}
