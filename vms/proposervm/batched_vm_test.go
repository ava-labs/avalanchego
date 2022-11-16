// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"crypto"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

func TestCoreVMNotRemote(t *testing.T) {
	// if coreVM is not remote VM, a specific error is returned
	require := require.New(t)
	_, _, proVM, _, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks

	blkID := ids.Empty
	maxBlocksNum := 1000                            // an high value to get all built blocks
	maxBlocksSize := 1000000                        // an high value to get all built blocks
	maxBlocksRetrivalTime := time.Duration(1000000) // an high value to get all built blocks
	_, errAncestors := proVM.GetAncestors(
		context.Background(),
		blkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.Error(errAncestors)

	var blks [][]byte
	_, errBatchedParse := proVM.BatchedParseBlock(context.Background(), blks)
	require.Error(errBatchedParse)
}

func TestGetAncestorsPreForkOnly(t *testing.T) {
	require := require.New(t)
	coreVM, proRemoteVM, coreGenBlk := initTestRemoteProposerVM(t, mockable.MaxTime) // disable ProBlks

	// Build some prefork blocks....
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build preFork block")

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

	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(222),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    coreBlk1.ID(),
		HeightV:    coreBlk1.Height() + 1,
		TimestampV: coreBlk1.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")

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

	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(222),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    coreBlk2.ID(),
		HeightV:    coreBlk2.Height() + 1,
		TimestampV: coreBlk2.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")

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
	maxBlocksNum := 1000                            // an high value to get all built blocks
	maxBlocksSize := 1000000                        // an high value to get all built blocks
	maxBlocksRetrivalTime := time.Duration(1000000) // an high value to get all built blocks
	res, err := proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)

	// ... and check returned values are as expected
	require.NoError(err, "Error calling GetAncestors: %v", err)
	require.Len(res, 3, "GetAncestor returned %v entries instead of %v", len(res), 3)
	require.EqualValues(res[0], builtBlk3.Bytes())
	require.EqualValues(res[1], builtBlk2.Bytes())
	require.EqualValues(res[2], builtBlk1.Bytes())

	// another good call
	reqBlkID = builtBlk1.ID()
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err, "Error calling GetAncestors: %v", err)
	require.Len(res, 1, "GetAncestor returned %v entries instead of %v", len(res), 1)
	require.EqualValues(res[0], builtBlk1.Bytes())

	// a faulty call
	reqBlkID = ids.Empty
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err, "Error calling GetAncestors: %v", err)
	require.Empty(res, "GetAncestor returned %v entries instead of %v", len(res), 0)
}

func TestGetAncestorsPostForkOnly(t *testing.T) {
	require := require.New(t)
	coreVM, proRemoteVM, coreGenBlk := initTestRemoteProposerVM(t, time.Time{}) // enable ProBlks

	// Build some post-Fork blocks....
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build preFork block")

	// prepare build of next block
	require.NoError(builtBlk1.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	proRemoteVM.Set(proRemoteVM.Time().Add(proposer.MaxDelay))

	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(222),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    coreBlk1.ID(),
		HeightV:    coreBlk1.Height() + 1,
		TimestampV: coreBlk1.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")

	// prepare build of next block
	require.NoError(builtBlk2.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	proRemoteVM.Set(proRemoteVM.Time().Add(proposer.MaxDelay))

	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(333),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    coreBlk2.ID(),
		HeightV:    coreBlk2.Height() + 1,
		TimestampV: coreBlk2.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")

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
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
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
	maxBlocksNum := 1000                            // an high value to get all built blocks
	maxBlocksSize := 1000000                        // an high value to get all built blocks
	maxBlocksRetrivalTime := time.Duration(1000000) // an high value to get all built blocks
	res, err := proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)

	// ... and check returned values are as expected
	require.NoError(err, "Error calling GetAncestors: %v", err)
	require.Len(res, 3)
	require.EqualValues(res[0], builtBlk3.Bytes())
	require.EqualValues(res[1], builtBlk2.Bytes())
	require.EqualValues(res[2], builtBlk1.Bytes())

	// another good call
	reqBlkID = builtBlk1.ID()
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err, "Error calling GetAncestors: %v", err)
	require.Len(res, 1, "GetAncestor returned %v entries instead of %v", len(res), 1)
	require.EqualValues(res[0], builtBlk1.Bytes())

	// a faulty call
	reqBlkID = ids.Empty
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err, "Error calling GetAncestors: %v", err)
	require.Empty(res, "GetAncestor returned %v entries instead of %v", len(res), 0)
}

func TestGetAncestorsAtSnomanPlusPlusFork(t *testing.T) {
	require := require.New(t)
	currentTime := time.Now().Truncate(time.Second)
	preForkTime := currentTime.Add(5 * time.Minute)
	forkTime := currentTime.Add(10 * time.Minute)
	postForkTime := currentTime.Add(15 * time.Minute)
	// enable ProBlks in next future
	coreVM, proRemoteVM, coreGenBlk := initTestRemoteProposerVM(t, forkTime)

	// Build some prefork blocks....
	proRemoteVM.Set(preForkTime)
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: preForkTime,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build preFork block")
	_, ok := builtBlk1.(*preForkBlock)
	require.True(ok, "Block should be a pre-fork one")

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

	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(222),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    coreBlk1.ID(),
		HeightV:    coreBlk1.Height() + 1,
		TimestampV: postForkTime,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")
	_, ok = builtBlk2.(*preForkBlock)
	require.True(ok, "Block should be a pre-fork one")

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
	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(333),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    coreBlk2.ID(),
		HeightV:    coreBlk2.Height() + 1,
		TimestampV: postForkTime.Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")
	_, ok = builtBlk3.(*postForkBlock)
	require.True(ok, "Block should be a post-fork one")

	// prepare build of next block
	require.NoError(builtBlk3.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk3.ID()))
	proRemoteVM.Set(proRemoteVM.Time().Add(proposer.MaxDelay))

	coreBlk4 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(444),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{4},
		ParentV:    coreBlk3.ID(),
		HeightV:    coreBlk3.Height() + 1,
		TimestampV: postForkTime,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk4, nil
	}
	builtBlk4, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")
	_, ok = builtBlk4.(*postForkBlock)
	require.True(ok, "Block should be a post-fork one")
	require.NoError(builtBlk4.Verify(context.Background()))

	// ...Call GetAncestors on them ...
	// Note: we assumed that if blkID is not known, that's NOT an error.
	// Simply return an empty result
	coreVM.GetAncestorsF = func(_ context.Context, blkID ids.ID, _, _ int, _ time.Duration) ([][]byte, error) {
		res := make([][]byte, 0, 3)
		switch blkID {
		case coreBlk4.ID():
			res = append(res, coreBlk4.Bytes())
			res = append(res, coreBlk3.Bytes())
			res = append(res, coreBlk2.Bytes())
			res = append(res, coreBlk1.Bytes())
			return res, nil
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
	require.NoError(err, "Error calling GetAncestors: %v", err)
	require.Len(res, 4, "GetAncestor returned %v entries instead of %v", len(res), 4)
	require.EqualValues(res[0], builtBlk4.Bytes())
	require.EqualValues(res[1], builtBlk3.Bytes())
	require.EqualValues(res[2], builtBlk2.Bytes())
	require.EqualValues(res[3], builtBlk1.Bytes())

	// another good call
	reqBlkID = builtBlk1.ID()
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err, "Error calling GetAncestors: %v", err)
	require.Len(res, 1, "GetAncestor returned %v entries instead of %v", len(res), 1)
	require.EqualValues(res[0], builtBlk1.Bytes())

	// a faulty call
	reqBlkID = ids.Empty
	res, err = proRemoteVM.GetAncestors(
		context.Background(),
		reqBlkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.NoError(err, "Error calling GetAncestors: %v", err)
	require.Len(res, 0, "GetAncestor returned %v entries instead of %v", len(res), 0)
}

func TestBatchedParseBlockPreForkOnly(t *testing.T) {
	require := require.New(t)
	coreVM, proRemoteVM, coreGenBlk := initTestRemoteProposerVM(t, mockable.MaxTime) // disable ProBlks

	// Build some prefork blocks....
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build preFork block")

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

	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(222),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    coreBlk1.ID(),
		HeightV:    coreBlk1.Height() + 1,
		TimestampV: coreBlk1.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")

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

	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(222),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    coreBlk2.ID(),
		HeightV:    coreBlk2.Height() + 1,
		TimestampV: coreBlk2.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")

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
				return nil, errors.New("Unexpected call to parse unknown block")
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
	require.NoError(err, "Error calling BatchedParseBlock: %v", err)
	require.Len(res, 3, "BatchedParseBlock returned %v entries instead of %v", len(res), 3)
	require.Equal(res[0].ID(), builtBlk1.ID())
	require.Equal(res[1].ID(), builtBlk2.ID())
	require.Equal(res[2].ID(), builtBlk3.ID())
}

func TestBatchedParseBlockPostForkOnly(t *testing.T) {
	require := require.New(t)
	coreVM, proRemoteVM, coreGenBlk := initTestRemoteProposerVM(t, time.Time{}) // enable ProBlks

	// Build some post-Fork blocks....
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build preFork block")

	// prepare build of next block
	require.NoError(builtBlk1.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk1.ID()))
	proRemoteVM.Set(proRemoteVM.Time().Add(proposer.MaxDelay))

	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(222),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    coreBlk1.ID(),
		HeightV:    coreBlk1.Height() + 1,
		TimestampV: coreBlk1.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")

	// prepare build of next block
	require.NoError(builtBlk2.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk2.ID()))
	proRemoteVM.Set(proRemoteVM.Time().Add(proposer.MaxDelay))

	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(333),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    coreBlk2.ID(),
		HeightV:    coreBlk2.Height() + 1,
		TimestampV: coreBlk2.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")

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
				return nil, errors.New("Unexpected call to parse unknown block")
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
	require.NoError(err, "Error calling BatchedParseBlock: %v", err)
	require.Len(res, 3, "BatchedParseBlock returned %v entries instead of %v", len(res), 3)
	require.Equal(res[0].ID(), builtBlk1.ID())
	require.Equal(res[1].ID(), builtBlk2.ID())
	require.Equal(res[2].ID(), builtBlk3.ID())
}

func TestBatchedParseBlockAtSnomanPlusPlusFork(t *testing.T) {
	require := require.New(t)
	currentTime := time.Now().Truncate(time.Second)
	preForkTime := currentTime.Add(5 * time.Minute)
	forkTime := currentTime.Add(10 * time.Minute)
	postForkTime := currentTime.Add(15 * time.Minute)
	// enable ProBlks in next future
	coreVM, proRemoteVM, coreGenBlk := initTestRemoteProposerVM(t, forkTime)

	// Build some prefork blocks....
	proRemoteVM.Set(preForkTime)
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: preForkTime,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build preFork block")
	_, ok := builtBlk1.(*preForkBlock)
	require.True(ok, "Block should be a pre-fork one")

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

	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(222),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    coreBlk1.ID(),
		HeightV:    coreBlk1.Height() + 1,
		TimestampV: postForkTime,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")
	_, ok = builtBlk2.(*preForkBlock)
	require.True(ok, "Block should be a pre-fork one")

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
	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(333),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    coreBlk2.ID(),
		HeightV:    coreBlk2.Height() + 1,
		TimestampV: postForkTime.Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")
	_, ok = builtBlk3.(*postForkBlock)
	require.True(ok, "Block should be a post-fork one")

	// prepare build of next block
	require.NoError(builtBlk3.Verify(context.Background()))
	require.NoError(proRemoteVM.SetPreference(context.Background(), builtBlk3.ID()))
	proRemoteVM.Set(proRemoteVM.Time().Add(proposer.MaxDelay))

	coreBlk4 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(444),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{4},
		ParentV:    coreBlk3.ID(),
		HeightV:    coreBlk3.Height() + 1,
		TimestampV: postForkTime,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk4, nil
	}
	builtBlk4, err := proRemoteVM.BuildBlock(context.Background())
	require.NoError(err, "Could not build proposer block")
	_, ok = builtBlk4.(*postForkBlock)
	require.True(ok, "Block should be a post-fork one")
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
				return nil, errors.New("Unexpected call to parse unknown block")
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
	require.NoError(err, "Error calling BatchedParseBlock: %v", err)
	require.Len(res, 4, "BatchedParseBlock returned %v entries instead of %v", len(res), 4)
	require.Equal(res[0].ID(), builtBlk4.ID())
	require.Equal(res[1].ID(), builtBlk3.ID())
	require.Equal(res[2].ID(), builtBlk2.ID())
	require.Equal(res[3].ID(), builtBlk1.ID())
}

type TestRemoteProposerVM struct {
	*block.TestBatchedVM
	*block.TestVM
}

func initTestRemoteProposerVM(
	t *testing.T,
	proBlkStartTime time.Time,
) (
	TestRemoteProposerVM,
	*VM,
	*snowman.TestBlock,
) {
	coreGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Accepted,
		},
		HeightV:    0,
		TimestampV: genesisTimestamp,
		BytesV:     []byte{0},
	}

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
		manager.Manager,
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
		return coreGenBlk.ID(), nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch {
		case blkID == coreGenBlk.ID():
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM := New(coreVM, proBlkStartTime, 0, DefaultMinBlockDelay)

	valState := &validators.TestState{
		T: t,
	}
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return coreGenBlk.Height(), nil
	}
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return defaultPChainHeight, nil
	}
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]uint64, error) {
		res := make(map[ids.NodeID]uint64)
		res[proVM.ctx.NodeID] = uint64(10)
		res[ids.NodeID{1}] = uint64(5)
		res[ids.NodeID{2}] = uint64(6)
		res[ids.NodeID{3}] = uint64(7)
		return res, nil
	}

	ctx := snow.DefaultContextTest()
	ctx.NodeID = ids.NodeIDFromCert(pTestCert.Leaf)
	ctx.StakingCertLeaf = pTestCert.Leaf
	ctx.StakingLeafSigner = pTestCert.PrivateKey.(crypto.Signer)
	ctx.ValidatorState = valState

	dummyDBManager := manager.NewMemDB(version.Semantic1_0_0)
	// make sure that DBs are compressed correctly
	dummyDBManager = dummyDBManager.NewPrefixDBManager([]byte{})
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
	if err != nil {
		t.Fatalf("failed to initialize proposerVM with %s", err)
	}

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	if err := proVM.SetState(context.Background(), snow.NormalOp); err != nil {
		t.Fatal(err)
	}

	if err := proVM.SetPreference(context.Background(), coreGenBlk.IDV); err != nil {
		t.Fatal(err)
	}

	return coreVM, proVM, coreGenBlk
}
