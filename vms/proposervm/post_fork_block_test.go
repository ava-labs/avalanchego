// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/stretchr/testify/require"
)

var errDuplicateVerify = errors.New("duplicate verify")

// ProposerBlock Option interface tests section
func TestOracle_PostForkBlock_ImplementsInterface(t *testing.T) {
	// setup
	require := require.New(t)
	proBlk := postForkBlock{
		postForkCommonComponents: postForkCommonComponents{
			innerBlk: &snowman.TestBlock{},
		},
	}

	// test
	_, err := proBlk.Options(context.Background())
	require.True(errors.Is(err, snowman.ErrNotOracle))

	// setup
	_, _, proVM, _, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	innerOracleBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV: ids.Empty.Prefix(1111),
			},
			BytesV: []byte{1},
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV: ids.Empty.Prefix(2222),
				},
				BytesV: []byte{2},
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV: ids.Empty.Prefix(3333),
				},
				BytesV: []byte{3},
			},
		},
	}

	slb, err := block.BuildBlsSigned(
		ids.Empty, // refer unknown parent
		time.Time{},
		0, // pChainHeight,
		proVM.ctx.NodeID,
		innerOracleBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.blsSigner,
	)
	require.NoError(err)

	proBlk = postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: innerOracleBlk,
			status:   choices.Processing,
		},
	}

	// test
	_, err = proBlk.Options(context.Background())
	require.NoError(err)
}

// ProposerBlock.Verify tests section
func TestBlockVerify_PostForkBlock_ParentChecks(t *testing.T) {
	require := require.New(t)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	// create parent block ...
	prntCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp(),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return prntCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case prntCoreBlk.ID():
			return prntCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, prntCoreBlk.Bytes()):
			return prntCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	proVM.Set(proVM.Time().Add(proposer.MaxDelay))
	prntProBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	err = prntProBlk.Verify(context.Background())
	require.NoError(err)

	err = proVM.SetPreference(context.Background(), prntProBlk.ID())
	require.NoError(err)

	// .. create child block ...
	childCoreBlk := &snowman.TestBlock{
		ParentV:    prntCoreBlk.ID(),
		BytesV:     []byte{2},
		TimestampV: prntCoreBlk.Timestamp(),
	}
	childSlb, err := block.BuildBlsSigned(
		ids.Empty, // refer unknown parent
		childCoreBlk.Timestamp(),
		pChainHeight,
		proVM.ctx.NodeID,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.blsSigner,
	)
	require.NoError(err)

	childProBlk := postForkBlock{
		SignedBlock: childSlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
			status:   choices.Processing,
		},
	}

	// child block referring unknown parent does not verify
	err = childProBlk.Verify(context.Background())
	require.True(errors.Is(err, database.ErrNotFound))

	// child block referring known parent does verify
	childSlb, err = block.BuildUnsigned(
		prntProBlk.ID(), // refer known parent
		prntProBlk.Timestamp().Add(proposer.MaxDelay),
		pChainHeight,
		childCoreBlk.Bytes(),
	)
	require.NoError(err)

	proVM.Set(proVM.Time().Add(proposer.MaxDelay))
	childProBlk.SignedBlock = childSlb
	err = childProBlk.Verify(context.Background())
	require.NoError(err)
}

func TestBlockVerify_PostForkBlock_TimestampChecks(t *testing.T) {
	require := require.New(t)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	pChainHeight := uint64(100)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	// create parent block ...
	prntCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return prntCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case prntCoreBlk.ID():
			return prntCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, prntCoreBlk.Bytes()):
			return prntCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	prntProBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	err = prntProBlk.Verify(context.Background())
	require.NoError(err)

	err = proVM.SetPreference(context.Background(), prntProBlk.ID())
	require.NoError(err)

	prntTimestamp := prntProBlk.Timestamp()

	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		ParentV: prntCoreBlk.ID(),
		BytesV:  []byte{2},
	}

	// child block timestamp cannot be lower than parent timestamp
	childCoreBlk.TimestampV = prntTimestamp.Add(-1 * time.Second)
	proVM.Clock.Set(childCoreBlk.TimestampV)
	childSlb, err := block.BuildBlsSigned(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		pChainHeight,
		proVM.ctx.NodeID,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.blsSigner,
	)
	require.NoError(err)

	childProBlk := postForkBlock{
		SignedBlock: childSlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
			status:   choices.Processing,
		},
	}

	// Proposer block timestamp too old
	err = childProBlk.Verify(context.Background())
	require.True(errors.Is(err, errTimeNotMonotonic))

	// block cannot arrive before its creator window starts
	blkWinDelay, _, err := proVM.DelayAndBlsKey(context.Background(), childCoreBlk.Height(), pChainHeight, proVM.ctx.NodeID)
	require.NoError(err)

	beforeWinStart := prntTimestamp.Add(blkWinDelay).Add(-1 * time.Second)
	proVM.Clock.Set(beforeWinStart)
	childSlb, err = block.BuildBlsSigned(
		prntProBlk.ID(),
		beforeWinStart,
		pChainHeight,
		proVM.ctx.NodeID,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.blsSigner,
	)
	require.NoError(err)

	childProBlk.SignedBlock = childSlb
	err = childProBlk.Verify(context.Background())
	require.True(errors.Is(err, errProposerWindowNotStarted))

	// block can arrive at its creator window starts
	atWindowStart := prntTimestamp.Add(blkWinDelay)
	proVM.Clock.Set(atWindowStart)
	childSlb, err = block.BuildBlsSigned(
		prntProBlk.ID(),
		atWindowStart,
		pChainHeight,
		proVM.ctx.NodeID,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.blsSigner,
	)
	require.NoError(err)

	childProBlk.SignedBlock = childSlb
	err = childProBlk.Verify(context.Background())
	require.NoError(err)

	// block can arrive after its creator window starts
	afterWindowStart := prntTimestamp.Add(blkWinDelay).Add(5 * time.Second)
	proVM.Clock.Set(afterWindowStart)
	childSlb, err = block.BuildBlsSigned(
		prntProBlk.ID(),
		afterWindowStart,
		pChainHeight,
		proVM.ctx.NodeID,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.blsSigner,
	)
	require.NoError(err)

	childProBlk.SignedBlock = childSlb
	err = childProBlk.Verify(context.Background())
	require.NoError(err)

	// block can arrive within submission window
	atSubWindowEnd := proVM.Time().Add(proposer.MaxDelay)
	proVM.Clock.Set(atSubWindowEnd)
	childSlb, err = block.BuildUnsigned(
		prntProBlk.ID(),
		atSubWindowEnd,
		pChainHeight,
		childCoreBlk.Bytes(),
	)
	require.NoError(err)

	childProBlk.SignedBlock = childSlb
	err = childProBlk.Verify(context.Background())
	require.NoError(err)

	// block timestamp cannot be too much in the future
	afterSubWinEnd := proVM.Time().Add(maxSkew).Add(time.Second)
	childSlb, err = block.BuildBlsSigned(
		prntProBlk.ID(),
		afterSubWinEnd,
		pChainHeight,
		proVM.ctx.NodeID,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.blsSigner,
	)
	require.NoError(err)

	childProBlk.SignedBlock = childSlb
	err = childProBlk.Verify(context.Background())
	require.True(errors.Is(err, errTimeTooAdvanced))
}

func TestBlockVerify_PostForkBlock_PChainHeightChecks(t *testing.T) {
	require := require.New(t)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks

	// create parent block ...
	prntCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return prntCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case prntCoreBlk.ID():
			return prntCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, prntCoreBlk.Bytes()):
			return prntCoreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	pChainHeight := uint64(100)
	valState.GetMinimumHeightF = func(ctx context.Context) (uint64, error) {
		return pChainHeight, nil
	}
	prntProBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	err = prntProBlk.Verify(context.Background())
	require.NoError(err)

	err = proVM.SetPreference(context.Background(), prntProBlk.ID())
	require.NoError(err)

	prntBlkPChainHeight := pChainHeight

	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		ParentV:    prntCoreBlk.ID(),
		BytesV:     []byte{2},
		TimestampV: prntProBlk.Timestamp().Add(proposer.MaxDelay),
	}

	// child P-Chain height must not precede parent P-Chain height
	childSlb, err := block.BuildBlsSigned(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight-1,
		proVM.ctx.NodeID,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.blsSigner,
	)
	require.NoError(err)

	childProBlk := postForkBlock{
		SignedBlock: childSlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
			status:   choices.Processing,
		},
	}
	err = childProBlk.Verify(context.Background())
	require.True(errors.Is(err, errPChainHeightNotMonotonic))

	// child P-Chain height can be equal to parent P-Chain height
	childSlb, err = block.BuildUnsigned(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight,
		childCoreBlk.Bytes(),
	)
	require.NoError(err)
	childProBlk.SignedBlock = childSlb

	proVM.Set(childCoreBlk.Timestamp())
	err = childProBlk.Verify(context.Background())
	require.NoError(err)

	// child P-Chain height may follow parent P-Chain height
	pChainHeight = prntBlkPChainHeight * 2 // move ahead pChainHeight
	childSlb, err = block.BuildUnsigned(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight+1,
		childCoreBlk.Bytes(),
	)
	require.NoError(err)

	childProBlk.SignedBlock = childSlb
	err = childProBlk.Verify(context.Background())
	require.NoError(err)

	// block P-Chain height can be equal to current P-Chain height
	currPChainHeight, _ := proVM.ctx.ValidatorState.GetCurrentHeight(context.Background())
	childSlb, err = block.BuildUnsigned(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		currPChainHeight,
		childCoreBlk.Bytes(),
	)
	require.NoError(err)

	childProBlk.SignedBlock = childSlb
	err = childProBlk.Verify(context.Background())
	require.NoError(err)

	// block P-Chain height cannot be at higher than current P-Chain height
	childSlb, err = block.BuildUnsigned(
		prntProBlk.ID(),
		childCoreBlk.Timestamp(),
		currPChainHeight*2,
		childCoreBlk.Bytes(),
	)
	require.NoError(err)

	childProBlk.SignedBlock = childSlb
	err = childProBlk.Verify(context.Background())
	require.True(errors.Is(err, errPChainHeightNotReached))
}

func TestBlockVerify_PostForkBlockBuiltOnOption_PChainHeightChecks(t *testing.T) {
	require := require.New(t)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	pChainHeight := uint64(100)
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}
	// proVM.SetStartTime(timer.MaxTime) // switch off scheduler for current test

	// create post fork oracle block ...
	oracleCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(1111),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
		},
	}
	oracleCoreBlk.opts = [2]snowman.Block{
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(2222),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{2},
			ParentV:    oracleCoreBlk.ID(),
			TimestampV: oracleCoreBlk.Timestamp(),
		},
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(3333),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{3},
			ParentV:    oracleCoreBlk.ID(),
			TimestampV: oracleCoreBlk.Timestamp(),
		},
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case oracleCoreBlk.ID():
			return oracleCoreBlk, nil
		case oracleCoreBlk.opts[0].ID():
			return oracleCoreBlk.opts[0], nil
		case oracleCoreBlk.opts[1].ID():
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, oracleCoreBlk.Bytes()):
			return oracleCoreBlk, nil
		case bytes.Equal(b, oracleCoreBlk.opts[0].Bytes()):
			return oracleCoreBlk.opts[0], nil
		case bytes.Equal(b, oracleCoreBlk.opts[1].Bytes()):
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	oracleBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	err = oracleBlk.Verify(context.Background())
	require.NoError(err)

	err = proVM.SetPreference(context.Background(), oracleBlk.ID())
	require.NoError(err)

	// retrieve one option and verify block built on it
	postForkOracleBlk, ok := oracleBlk.(*postForkBlock)
	require.True(ok)

	opts, err := postForkOracleBlk.Options(context.Background())
	require.NoError(err)
	parentBlk := opts[0]

	err = parentBlk.Verify(context.Background())
	require.NoError(err)

	err = proVM.SetPreference(context.Background(), parentBlk.ID())
	require.NoError(err)

	prntBlkPChainHeight := pChainHeight

	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		ParentV:    oracleCoreBlk.opts[0].ID(),
		BytesV:     []byte{2},
		TimestampV: parentBlk.Timestamp().Add(proposer.MaxDelay),
	}

	// child P-Chain height must not precede parent P-Chain height
	childSlb, err := block.BuildBlsSigned(
		parentBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight-1,
		proVM.ctx.NodeID,
		childCoreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.blsSigner,
	)
	require.NoError(err)

	childProBlk := postForkBlock{
		SignedBlock: childSlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: childCoreBlk,
			status:   choices.Processing,
		},
	}

	err = childProBlk.Verify(context.Background())
	require.True(errors.Is(err, errPChainHeightNotMonotonic))

	// child P-Chain height can be equal to parent P-Chain height
	childSlb, err = block.BuildUnsigned(
		parentBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight,
		childCoreBlk.Bytes(),
	)
	require.NoError(err)

	childProBlk.SignedBlock = childSlb
	proVM.Set(childCoreBlk.Timestamp())
	err = childProBlk.Verify(context.Background())
	require.NoError(err)

	// child P-Chain height may follow parent P-Chain height
	pChainHeight = prntBlkPChainHeight * 2 // move ahead pChainHeight
	childSlb, err = block.BuildUnsigned(
		parentBlk.ID(),
		childCoreBlk.Timestamp(),
		prntBlkPChainHeight+1,
		childCoreBlk.Bytes(),
	)
	require.NoError(err)

	childProBlk.SignedBlock = childSlb
	err = childProBlk.Verify(context.Background())
	require.NoError(err)

	// block P-Chain height can be equal to current P-Chain height
	currPChainHeight, _ := proVM.ctx.ValidatorState.GetCurrentHeight(context.Background())
	childSlb, err = block.BuildUnsigned(
		parentBlk.ID(),
		childCoreBlk.Timestamp(),
		currPChainHeight,
		childCoreBlk.Bytes(),
	)
	require.NoError(err)

	childProBlk.SignedBlock = childSlb
	err = childProBlk.Verify(context.Background())
	require.NoError(err)

	// block P-Chain height cannot be at higher than current P-Chain height
	childSlb, err = block.BuildUnsigned(
		parentBlk.ID(),
		childCoreBlk.Timestamp(),
		currPChainHeight*2,
		childCoreBlk.Bytes(),
	)
	require.NoError(err)

	childProBlk.SignedBlock = childSlb
	err = childProBlk.Verify(context.Background())
	require.True(errors.Is(err, errPChainHeightNotReached))
}

func TestBlockVerify_PostForkBlock_CoreBlockVerifyIsCalledOnce(t *testing.T) {
	// Verify a block once (in this test by building it).
	// Show that other verify call would not call coreBlk.Verify()
	require := require.New(t)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	pChainHeight := uint64(2000)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	builtBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	err = builtBlk.Verify(context.Background())
	require.NoError(err)

	// set error on coreBlock.Verify and recall Verify()
	coreBlk.VerifyV = errDuplicateVerify
	err = builtBlk.Verify(context.Background())
	require.NoError(err)

	// rebuild a block with the same core block
	pChainHeight++
	_, err = proVM.BuildBlock(context.Background())
	require.NoError(err)
}

// ProposerBlock.Accept tests section
func TestBlockAccept_PostForkBlock_SetsLastAcceptedBlock(t *testing.T) {
	// setup
	require := require.New(t)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	pChainHeight := uint64(2000)
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	builtBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// test
	err = builtBlk.Accept(context.Background())
	require.NoError(err)

	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		if coreBlk.Status() == choices.Accepted {
			return coreBlk.ID(), nil
		}
		return coreGenBlk.ID(), nil
	}
	acceptedID, err := proVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(acceptedID, builtBlk.ID())
}

func TestBlockAccept_PostForkBlock_TwoProBlocksWithSameCoreBlock_OneIsAccepted(t *testing.T) {
	require := require.New(t)
	coreVM, valState, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	var minimumHeight uint64
	valState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return minimumHeight, nil
	}

	// generate two blocks with the same core block and store them
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	minimumHeight = coreGenBlk.Height()

	proBlk1, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	minimumHeight++
	proBlk2, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.NotEqual(proBlk1.ID(), proBlk2.ID())

	// set proBlk1 as preferred
	err = proBlk1.Accept(context.Background())
	require.NoError(err)
	require.Equal(choices.Accepted, coreBlk.Status())

	acceptedID, err := proVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(acceptedID, proBlk1.ID())
}

// ProposerBlock.Reject tests section
func TestBlockReject_PostForkBlock_InnerBlockIsNotRejected(t *testing.T) {
	require := require.New(t)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		HeightV:    coreGenBlk.Height() + 1,
		TimestampV: coreGenBlk.Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	sb, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	proBlk, ok := sb.(*postForkBlock)
	require.True(ok)

	err = proBlk.Reject(context.Background())
	require.NoError(err)

	require.Equal(choices.Rejected, proBlk.Status())
	require.NotEqual(choices.Rejected, proBlk.innerBlk.Status())
}

func TestBlockVerify_PostForkBlock_ShouldBePostForkOption(t *testing.T) {
	require := require.New(t)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0)
	proVM.Set(coreGenBlk.Timestamp())

	// create post fork oracle block ...
	oracleCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(1111),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: coreGenBlk.Timestamp(),
		},
	}
	coreOpt0 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    oracleCoreBlk.ID(),
		TimestampV: oracleCoreBlk.Timestamp(),
	}
	coreOpt1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{3},
		ParentV:    oracleCoreBlk.ID(),
		TimestampV: oracleCoreBlk.Timestamp(),
	}
	oracleCoreBlk.opts = [2]snowman.Block{
		coreOpt0,
		coreOpt1,
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case oracleCoreBlk.ID():
			return oracleCoreBlk, nil
		case oracleCoreBlk.opts[0].ID():
			return oracleCoreBlk.opts[0], nil
		case oracleCoreBlk.opts[1].ID():
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, oracleCoreBlk.Bytes()):
			return oracleCoreBlk, nil
		case bytes.Equal(b, oracleCoreBlk.opts[0].Bytes()):
			return oracleCoreBlk.opts[0], nil
		case bytes.Equal(b, oracleCoreBlk.opts[1].Bytes()):
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	err = parentBlk.Verify(context.Background())
	require.NoError(err)

	err = proVM.SetPreference(context.Background(), parentBlk.ID())
	require.NoError(err)

	// retrieve options ...
	postForkOracleBlk, ok := parentBlk.(*postForkBlock)
	require.True(ok)

	opts, err := postForkOracleBlk.Options(context.Background())
	require.NoError(err)

	_, ok = opts[0].(*postForkOption)
	require.True(ok)

	// ... and verify them the first time
	err = opts[0].Verify(context.Background())
	require.NoError(err)

	err = opts[1].Verify(context.Background())
	require.NoError(err)

	// Build the child
	statelessChild, err := block.BuildBlsSigned(
		postForkOracleBlk.ID(),
		postForkOracleBlk.Timestamp().Add(proposer.WindowDuration),
		postForkOracleBlk.PChainHeight(),
		proVM.ctx.NodeID,
		oracleCoreBlk.opts[0].Bytes(),
		proVM.ctx.ChainID,
		proVM.blsSigner,
	)
	require.NoError(err)

	invalidChild, err := proVM.ParseBlock(context.Background(), statelessChild.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify(context.Background())
	require.Error(err)
}

func TestBlockVerify_PostForkBlock_PChainTooLow(t *testing.T) {
	require := require.New(t)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 5)
	proVM.Set(coreGenBlk.Timestamp())

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp(),
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	statelessChild, err := block.BuildUnsigned(
		coreGenBlk.ID(),
		coreGenBlk.Timestamp(),
		4,
		coreBlk.Bytes(),
	)
	require.NoError(err)

	invalidChild, err := proVM.ParseBlock(context.Background(), statelessChild.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify(context.Background())
	require.True(errors.Is(err, errPChainHeightTooLow))
}
