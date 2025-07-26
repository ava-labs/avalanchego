// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
)

func equal(require *require.Assertions, want, have Block) {
	require.Equal(want.ID(), have.ID())
	require.Equal(want.ParentID(), have.ParentID())
	require.Equal(want.Block(), have.Block())
	require.Equal(want.Bytes(), have.Bytes())

	signedWant, wantIsSigned := want.(SignedBlock)
	signedHave, haveIsSigned := have.(SignedBlock)
	require.Equal(wantIsSigned, haveIsSigned)
	if !wantIsSigned {
		return
	}

	require.Equal(signedWant.PChainHeight(), signedHave.PChainHeight())
	require.Equal(signedWant.Timestamp(), signedHave.Timestamp())
	require.Equal(signedWant.Proposer(), signedHave.Proposer())
}

func TestBlockSizeLimit(t *testing.T) {
	require := require.New(t)

	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := bytes.Repeat([]byte{0}, 270*units.KiB)

	// with the large limit, it should be able to build large blocks
	_, err := BuildUnsigned(parentID, timestamp, pChainHeight, 0, 0, time.Time{}, innerBlockBytes)
	require.NoError(err)
}
