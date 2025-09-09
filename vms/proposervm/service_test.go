// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

	service := &service{vm: proVM}

	proposedHeightResponse, err := service.GetProposedHeight(context.TODO(), nil)
	require.NoError(err)
	minHeight, err := service.vm.ctx.ValidatorState.GetMinimumHeight(context.Background())
	require.NoError(err)
	require.Equal(minHeight, proposedHeightResponse.Msg.Height)
}
