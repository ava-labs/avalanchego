// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

func TestCoreVMNotRemote(t *testing.T) {
	// if coreVM is not remote VM, a specific error is returned
	require := require.New(t)
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	_, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	blkID := ids.Empty
	maxBlocksNum := 1000               // a high value to get all built blocks
	maxBlocksSize := 1000000           // a high value to get all built blocks
	maxBlocksRetrivalTime := time.Hour // a high value to get all built blocks
	_, err := proVM.GetAncestors(
		context.Background(),
		blkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.ErrorIs(err, block.ErrRemoteVMNotImplemented)

	var blks [][]byte
	_, err = proVM.BatchedParseBlock(context.Background(), blks)
	require.ErrorIs(err, block.ErrRemoteVMNotImplemented)
}

func TestGetAncestorsPreForkOnly(t *testing.T) {
	require := require.New(t)
	var (
		activationTime = mockable.MaxTime
		durangoTime    = activationTime
	)
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, activationTime, durangoTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(context.Background()))
	}()

	// Build some prefork blocks....
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreBlk2 := snowmantest.BuildChild(coreBlk1)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreBlk2.ID():
			return coreBlk2, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreBlk3 := snowmantest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// ...Call GetAncestors on them ...
	// Note: we assumed that if blkID is not known, that's NOT an error.
	// Simply return an empty result
	coreVM.GetAncestorsF = func(_ context.Context, blkID ids.ID, _, _ int, _ time.Duration) ([][]byte, error) {
		res := make([][]byte, 0, 3)
		switch blkID {
		case coreBlk3.ID():
			res = append(res, coreBlk3.Bytes())
			res = append(res, coreBlk2.Bytes())
			res = append(res, coreBlk1.Bytes())
			return res, nil
		case coreBlk2.ID():
			res = append(res, coreBlk2.Bytes())
			res = append(res, coreBlk1.Bytes())
			return res, nil
		case coreBlk1.ID():
			res = append(res, coreBlk1.Bytes())
			return res, nil
		default:
			return res, nil
		}
	}

	reqBlkID := builtBlk3.ID()
	maxBlocksNum := 1000               // a high value to get all built blocks
	maxBlocksSize := 1000000           // a high value to get all built blocks
	maxBlocksRetrivalTime := time.Hour // a high value to get all built blocks
	res, err := proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)

	// ... and check returned values are as expected
	require.NoError(err)
	require.Len(res, 3)
	require.Equal(builtBlk3.Bytes(), res[0])
	require.Equal(builtBlk2.Bytes(), res[1])
	require.Equal(builtBlk1.Bytes(), res[2])

	// another good call
	reqBlkID = builtBlk1.ID()
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err)
	require.Len(res, 1)
	require.Equal(builtBlk1.Bytes(), res[0])

	// a faulty call
	reqBlkID = ids.Empty
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err)
	require.Empty(res)
}

func TestGetAncestorsPostForkOnly(t *testing.T) {
	require := require.New(t)
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, activationTime, durangoTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(context.Background()))
	}()

	// Build some post-Fork blocks....
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(builtBlk1.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	require.NoError(waitForProposerWindow(proRemoteVM, builtBlk1, 0))

	coreBlk2 := snowmantest.BuildChild(coreBlk1)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(builtBlk2.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	require.NoError(waitForProposerWindow(proRemoteVM, builtBlk2, 0))

	coreBlk3 := snowmantest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(builtBlk3.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk3.ID()))

	// ...Call GetAncestors on them ...
	// Note: we assumed that if blkID is not known, that's NOT an error.
	// Simply return an empty result
	coreVM.GetAncestorsF = func(_ context.Context, blkID ids.ID, _, _ int, _ time.Duration) ([][]byte, error) {
		res := make([][]byte, 0, 3)
		switch blkID {
		case coreBlk3.ID():
			res = append(res, coreBlk3.Bytes())
			res = append(res, coreBlk2.Bytes())
			res = append(res, coreBlk1.Bytes())
			return res, nil
		case coreBlk2.ID():
			res = append(res, coreBlk2.Bytes())
			res = append(res, coreBlk1.Bytes())
			return res, nil
		case coreBlk1.ID():
			res = append(res, coreBlk1.Bytes())
			return res, nil
		default:
			return res, nil
		}
	}

	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.Bytes()):
			return coreBlk2, nil
		case bytes.Equal(b, coreBlk3.Bytes()):
			return coreBlk3, nil
		default:
			return nil, errUnknownBlock
		}
	}

	reqBlkID := builtBlk3.ID()
	maxBlocksNum := 1000               // a high value to get all built blocks
	maxBlocksSize := 1000000           // a high value to get all built blocks
	maxBlocksRetrivalTime := time.Hour // a high value to get all built blocks
	res, err := proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)

	// ... and check returned values are as expected
	require.NoError(err)
	require.Len(res, 3)
	require.Equal(builtBlk3.Bytes(), res[0])
	require.Equal(builtBlk2.Bytes(), res[1])
	require.Equal(builtBlk1.Bytes(), res[2])

	// another good call
	reqBlkID = builtBlk1.ID()
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err)
	require.Len(res, 1)
	require.Equal(builtBlk1.Bytes(), res[0])

	// a faulty call
	reqBlkID = ids.Empty
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err)
	require.Empty(res)
}

func TestGetAncestorsAtSnomanPlusPlusFork(t *testing.T) {
	require := require.New(t)

	var (
		currentTime  = time.Now().Truncate(time.Second)
		preForkTime  = currentTime.Add(5 * time.Minute)
		forkTime     = currentTime.Add(10 * time.Minute)
		postForkTime = currentTime.Add(15 * time.Minute)

		durangoTime = forkTime
	)

	// enable ProBlks in next future
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, forkTime, durangoTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(context.Background()))
	}()

	// Build some prefork blocks....
	proRemoteVM.Set(preForkTime)
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreBlk1.TimestampV = preForkTime
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk1)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID == coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreBlk2 := snowmantest.BuildChild(coreBlk1)
	coreBlk2.TimestampV = postForkTime
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk2)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID == coreBlk2.ID():
			return coreBlk2, nil
		default:
			return nil, errUnknownBlock
		}
	}

	// .. and some post-fork
	proRemoteVM.Set(postForkTime)
	coreBlk3 := snowmantest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk3)

	// prepare build of next block
	require.NoError(builtBlk3.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk3.ID()))
	require.NoError(waitForProposerWindow(proRemoteVM, builtBlk3, builtBlk3.(*postForkBlock).PChainHeight()))

	coreBlk4 := snowmantest.BuildChild(coreBlk3)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk4, nil
	}
	builtBlk4, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk4)
	require.NoError(builtBlk4.Verify(context.Background()))

	// ...Call GetAncestors on them ...
	// Note: we assumed that if blkID is not known, that's NOT an error.
	// Simply return an empty result
	coreVM.GetAncestorsF = func(_ context.Context, blkID ids.ID, maxBlocksNum, _ int, _ time.Duration) ([][]byte, error) {
		sortedBlocks := [][]byte{
			coreBlk4.Bytes(),
			coreBlk3.Bytes(),
			coreBlk2.Bytes(),
			coreBlk1.Bytes(),
		}
		var startIndex int
		switch blkID {
		case coreBlk4.ID():
			startIndex = 0
		case coreBlk3.ID():
			startIndex = 1
		case coreBlk2.ID():
			startIndex = 2
		case coreBlk1.ID():
			startIndex = 3
		default:
			return nil, nil // unknown blockID
		}

		endIndex := min(startIndex+maxBlocksNum, len(sortedBlocks))
		return sortedBlocks[startIndex:endIndex], nil
	}

	// load all known blocks
	reqBlkID := builtBlk4.ID()
	maxBlocksNum := 1000                      // an high value to get all built blocks
	maxBlocksSize := 1000000                  // an high value to get all built blocks
	maxBlocksRetrivalTime := 10 * time.Minute // an high value to get all built blocks
	res, err := proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)

	// ... and check returned values are as expected
	require.NoError(err)
	require.Len(res, 4)
	require.Equal(builtBlk4.Bytes(), res[0])
	require.Equal(builtBlk3.Bytes(), res[1])
	require.Equal(builtBlk2.Bytes(), res[2])
	require.Equal(builtBlk1.Bytes(), res[3])

	// Regression case: load some prefork and some postfork blocks.
	reqBlkID = builtBlk4.ID()
	maxBlocksNum = 3
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)

	// ... and check returned values are as expected
	require.NoError(err)
	require.Len(res, 3)
	require.Equal(builtBlk4.Bytes(), res[0])
	require.Equal(builtBlk3.Bytes(), res[1])
	require.Equal(builtBlk2.Bytes(), res[2])

	// another good call
	reqBlkID = builtBlk1.ID()
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err)
	require.Len(res, 1)
	require.Equal(builtBlk1.Bytes(), res[0])

	// a faulty call
	reqBlkID = ids.Empty
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err)
	require.Empty(res)
}

func TestBatchedParseBlockPreForkOnly(t *testing.T) {
	require := require.New(t)
	var (
		activationTime = mockable.MaxTime
		durangoTime    = activationTime
	)
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, activationTime, durangoTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(context.Background()))
	}()

	// Build some prefork blocks....
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreBlk2 := snowmantest.BuildChild(coreBlk1)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID == coreBlk2.ID():
			return coreBlk2, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreBlk3 := snowmantest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.Bytes()):
			return coreBlk2, nil
		case bytes.Equal(b, coreBlk3.Bytes()):
			return coreBlk3, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreVM.BatchedParseBlockF = func(_ context.Context, blks [][]byte) ([]snowman.Block, error) {
		res := make([]snowman.Block, 0, len(blks))
		for _, blkBytes := range blks {
			switch {
			case bytes.Equal(blkBytes, coreBlk1.Bytes()):
				res = append(res, coreBlk1)
			case bytes.Equal(blkBytes, coreBlk2.Bytes()):
				res = append(res, coreBlk2)
			case bytes.Equal(blkBytes, coreBlk3.Bytes()):
				res = append(res, coreBlk3)
			default:
				return nil, errUnknownBlock
			}
		}
		return res, nil
	}

	bytesToParse := [][]byte{
		builtBlk1.Bytes(),
		builtBlk2.Bytes(),
		builtBlk3.Bytes(),
	}
	res, err := proRemoteVM.BatchedParseBlock(context.Background(), bytesToParse)
	require.NoError(err)
	require.Len(res, 3)
	require.Equal(builtBlk1.ID(), res[0].ID())
	require.Equal(builtBlk2.ID(), res[1].ID())
	require.Equal(builtBlk3.ID(), res[2].ID())
}

func TestBatchedParseBlockPostForkOnly(t *testing.T) {
	require := require.New(t)
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, activationTime, durangoTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(context.Background()))
	}()

	// Build some post-Fork blocks....
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(builtBlk1.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	require.NoError(waitForProposerWindow(proRemoteVM, builtBlk1, 0))

	coreBlk2 := snowmantest.BuildChild(coreBlk1)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	// prepare build of next block
	require.NoError(builtBlk2.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	require.NoError(waitForProposerWindow(proRemoteVM, builtBlk2, builtBlk2.(*postForkBlock).PChainHeight()))

	coreBlk3 := snowmantest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)

	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.Bytes()):
			return coreBlk2, nil
		case bytes.Equal(b, coreBlk3.Bytes()):
			return coreBlk3, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreVM.BatchedParseBlockF = func(_ context.Context, blks [][]byte) ([]snowman.Block, error) {
		res := make([]snowman.Block, 0, len(blks))
		for _, blkBytes := range blks {
			switch {
			case bytes.Equal(blkBytes, coreBlk1.Bytes()):
				res = append(res, coreBlk1)
			case bytes.Equal(blkBytes, coreBlk2.Bytes()):
				res = append(res, coreBlk2)
			case bytes.Equal(blkBytes, coreBlk3.Bytes()):
				res = append(res, coreBlk3)
			default:
				return nil, errUnknownBlock
			}
		}
		return res, nil
	}

	bytesToParse := [][]byte{
		builtBlk1.Bytes(),
		builtBlk2.Bytes(),
		builtBlk3.Bytes(),
	}
	res, err := proRemoteVM.BatchedParseBlock(context.Background(), bytesToParse)
	require.NoError(err)
	require.Len(res, 3)
	require.Equal(builtBlk1.ID(), res[0].ID())
	require.Equal(builtBlk2.ID(), res[1].ID())
	require.Equal(builtBlk3.ID(), res[2].ID())
}

func TestBatchedParseBlockAtSnomanPlusPlusFork(t *testing.T) {
	require := require.New(t)

	var (
		currentTime  = time.Now().Truncate(time.Second)
		preForkTime  = currentTime.Add(5 * time.Minute)
		forkTime     = currentTime.Add(10 * time.Minute)
		postForkTime = currentTime.Add(15 * time.Minute)

		durangoTime = forkTime
	)

	// enable ProBlks in next future
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, forkTime, durangoTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(context.Background()))
	}()

	// Build some prefork blocks....
	proRemoteVM.Set(preForkTime)
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreBlk1.TimestampV = preForkTime
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk1)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID == coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreBlk2 := snowmantest.BuildChild(coreBlk1)
	coreBlk2.TimestampV = postForkTime
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk2)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID == coreBlk2.ID():
			return coreBlk2, nil
		default:
			return nil, errUnknownBlock
		}
	}

	// .. and some post-fork
	proRemoteVM.Set(postForkTime)
	coreBlk3 := snowmantest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk3)

	// prepare build of next block
	require.NoError(builtBlk3.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk3.ID()))
	require.NoError(waitForProposerWindow(proRemoteVM, builtBlk3, builtBlk3.(*postForkBlock).PChainHeight()))

	coreBlk4 := snowmantest.BuildChild(coreBlk3)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk4, nil
	}
	builtBlk4, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk4)
	require.NoError(builtBlk4.Verify(context.Background()))

	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.Bytes()):
			return coreBlk2, nil
		case bytes.Equal(b, coreBlk3.Bytes()):
			return coreBlk3, nil
		case bytes.Equal(b, coreBlk4.Bytes()):
			return coreBlk4, nil
		default:
			return nil, errUnknownBlock
		}
	}

	coreVM.BatchedParseBlockF = func(_ context.Context, blks [][]byte) ([]snowman.Block, error) {
		res := make([]snowman.Block, 0, len(blks))
		for _, blkBytes := range blks {
			switch {
			case bytes.Equal(blkBytes, coreBlk1.Bytes()):
				res = append(res, coreBlk1)
			case bytes.Equal(blkBytes, coreBlk2.Bytes()):
				res = append(res, coreBlk2)
			case bytes.Equal(blkBytes, coreBlk3.Bytes()):
				res = append(res, coreBlk3)
			case bytes.Equal(blkBytes, coreBlk4.Bytes()):
				res = append(res, coreBlk4)
			default:
				return nil, errUnknownBlock
			}
		}
		return res, nil
	}

	bytesToParse := [][]byte{
		builtBlk4.Bytes(),
		builtBlk3.Bytes(),
		builtBlk2.Bytes(),
		builtBlk1.Bytes(),
	}

	res, err := proRemoteVM.BatchedParseBlock(context.Background(), bytesToParse)
	require.NoError(err)
	require.Len(res, 4)
	require.Equal(builtBlk4.ID(), res[0].ID())
	require.Equal(builtBlk3.ID(), res[1].ID())
	require.Equal(builtBlk2.ID(), res[2].ID())
	require.Equal(builtBlk1.ID(), res[3].ID())
}

type TestRemoteProposerVM struct {
	*block.TestBatchedVM
	*block.TestVM
}

func initTestRemoteProposerVM(
	t *testing.T,
	activationTime,
	durangoTime time.Time,
) (
	TestRemoteProposerVM,
	*VM,
) {
	require := require.New(t)

	initialState := []byte("genesis state")
	coreVM := TestRemoteProposerVM{
		TestVM:        &block.TestVM{},
		TestBatchedVM: &block.TestBatchedVM{},
	}
	coreVM.TestVM.T = t
	coreVM.TestBatchedVM.T = t

	coreVM.InitializeF = func(
		context.Context,
		*snow.Context,
		database.Database,
		[]byte,
		[]byte,
		[]byte,
		chan<- common.Message,
		[]*common.Fx,
		common.AppSender,
	) error {
		return nil
	}
	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return snowmantest.GenesisID, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM := New(
		coreVM,
		Config{
			ActivationTime:      activationTime,
			DurangoTime:         durangoTime,
			MinimumPChainHeight: 0,
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)

	valState := &validators.TestState{
		T: t,
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return snowmantest.GenesisHeight, nil
	}
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return defaultPChainHeight, nil
	}
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		var (
			thisNode = proVM.ctx.NodeID
			nodeID1  = ids.BuildTestNodeID([]byte{1})
			nodeID2  = ids.BuildTestNodeID([]byte{2})
			nodeID3  = ids.BuildTestNodeID([]byte{3})
		)
		return map[ids.NodeID]*validators.GetValidatorOutput{
			thisNode: {
				NodeID: thisNode,
				Weight: 10,
			},
			nodeID1: {
				NodeID: nodeID1,
				Weight: 5,
			},
			nodeID2: {
				NodeID: nodeID2,
				Weight: 6,
			},
			nodeID3: {
				NodeID: nodeID3,
				Weight: 7,
			},
		}, nil
	}

	ctx := snowtest.Context(t, snowtest.CChainID)
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)
	ctx.ValidatorState = valState

	require.NoError(proVM.Initialize(
		context.Background(),
		ctx,
		prefixdb.New([]byte{}, memdb.New()), // make sure that DBs are compressed correctly
		initialState,
		nil,
		nil,
		nil,
		nil,
		nil,
	))

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	require.NoError(proVM.SetState(context.Background(), snow.NormalOp))
	require.NoError(proVM.SetPreference(context.Background(), snowmantest.GenesisID))
	return coreVM, proVM
}
