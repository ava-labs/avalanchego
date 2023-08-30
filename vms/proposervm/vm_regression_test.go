// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/version"
)

func TestProposerVMInitializeShouldFailIfInnerVMCantVerifyItsHeightIndex(t *testing.T) {
	require := require.New(t)

	innerVM := &fullVM{
		TestVM: &block.TestVM{
			TestVM: common.TestVM{
				T: t,
			},
		},
	}

	// let innerVM fail verifying its height index with
	// a non-special error (like block.ErrIndexIncomplete)
	customError := errors.New("custom error")
	innerVM.VerifyHeightIndexF = func(_ context.Context) error {
		return customError
	}

	innerVM.InitializeF = func(context.Context, *snow.Context, manager.Manager,
		[]byte, []byte, []byte, chan<- common.Message,
		[]*common.Fx, common.AppSender,
	) error {
		return nil
	}

	proVM := New(
		innerVM,
		time.Time{},
		0,
		DefaultMinBlockDelay,
		DefaultNumHistoricalBlocks,
		pTestSigner,
		pTestCert,
	)
	defer func() {
		// avoids leaking goroutines
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	ctx := snow.DefaultContextTest()
	dummyDBManager := manager.NewMemDB(version.Semantic1_0_0)
	dummyDBManager = dummyDBManager.NewPrefixDBManager([]byte{})
	initialState := []byte("genesis state")

	err := proVM.Initialize(
		context.Background(),
		ctx,
		dummyDBManager,
		initialState,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	require.ErrorIs(customError, err)
}
