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
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
	"github.com/stretchr/testify/require"
)

var _ snowman.OracleBlock = (*TestOptionsBlock)(nil)

type TestOptionsBlock struct {
	snowman.TestBlock
	opts    [2]snowman.Block
	optsErr error
}

func (tob TestOptionsBlock) Options(context.Context) ([2]snowman.Block, error) {
	return tob.opts, tob.optsErr
}

// ProposerBlock.Verify tests section
func TestBlockVerify_PostForkOption_ParentChecks(t *testing.T) {
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

	// ... and verify them
	err = opts[0].Verify(context.Background())
	require.NoError(err)

	err = opts[1].Verify(context.Background())
	require.NoError(err)

	// show we can build on options
	err = proVM.SetPreference(context.Background(), opts[0].ID())
	require.NoError(err)

	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(4444),
			StatusV: choices.Processing,
		},
		ParentV:    oracleCoreBlk.opts[0].ID(),
		BytesV:     []byte{4},
		TimestampV: oracleCoreBlk.opts[0].Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return childCoreBlk, nil
	}
	proVM.Set(childCoreBlk.Timestamp())

	proChild, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	_, ok = proChild.(*postForkBlock)
	require.True(ok)

	err = proChild.Verify(context.Background())
	require.NoError(err)
}

// ProposerBlock.Accept tests section
func TestBlockVerify_PostForkOption_CoreBlockVerifyIsCalledOnce(t *testing.T) {
	// Verify an option once; then show that another verify call would not call coreBlk.Verify()
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

	// set error on coreBlock.Verify and recall Verify()
	coreOpt0.VerifyV = errDuplicateVerify
	coreOpt1.VerifyV = errDuplicateVerify

	// ... and verify them again. They verify without call to innerBlk
	err = opts[0].Verify(context.Background())
	require.NoError(err)

	err = opts[1].Verify(context.Background())
	require.NoError(err)
}

func TestBlockAccept_PostForkOption_SetsLastAcceptedBlock(t *testing.T) {
	// setup
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

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// accept oracle block
	err = parentBlk.Accept(context.Background())
	require.NoError(err)

	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		if oracleCoreBlk.Status() == choices.Accepted {
			return oracleCoreBlk.ID(), nil
		}
		return coreGenBlk.ID(), nil
	}
	acceptedID, err := proVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(acceptedID, parentBlk.ID())

	// accept one of the options
	postForkOracleBlk, ok := parentBlk.(*postForkBlock)
	require.True(ok)

	opts, err := postForkOracleBlk.Options(context.Background())
	require.NoError(err)

	err = opts[0].Accept(context.Background())
	require.NoError(err)

	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		if oracleCoreBlk.opts[0].Status() == choices.Accepted {
			return oracleCoreBlk.opts[0].ID(), nil
		}
		return oracleCoreBlk.ID(), nil
	}

	acceptedID, err = proVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(acceptedID, opts[0].ID())
}

// ProposerBlock.Reject tests section
func TestBlockReject_InnerBlockIsNotRejected(t *testing.T) {
	// setup
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

	builtBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// reject oracle block
	err = builtBlk.Reject(context.Background())
	require.NoError(err)

	proBlk, ok := builtBlk.(*postForkBlock)
	require.True(ok)
	require.Equal(choices.Rejected, proBlk.Status())
	require.NotEqual(choices.Rejected, proBlk.innerBlk.Status())

	// reject an option
	postForkOracleBlk, ok := builtBlk.(*postForkBlock)
	require.True(ok)

	opts, err := postForkOracleBlk.Options(context.Background())
	require.NoError(err)

	err = opts[0].Reject(context.Background())
	require.NoError(err)

	proOpt, ok := opts[0].(*postForkOption)
	require.True(ok)

	require.Equal(choices.Rejected, proOpt.Status())
	require.NotEqual(choices.Rejected, proOpt.innerBlk.Status())
}

func TestBlockVerify_PostForkOption_ParentIsNotOracleWithError(t *testing.T) {
	// Verify an option once; then show that another verify call would not call coreBlk.Verify()
	require := require.New(t)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, time.Time{}, 0)
	proVM.Set(coreGenBlk.Timestamp())

	coreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.GenerateTestID(),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: coreGenBlk.Timestamp(),
		},
		optsErr: snowman.ErrNotOracle,
	}

	coreChildBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		ParentV:    coreBlk.ID(),
		HeightV:    coreBlk.Height() + 1,
		TimestampV: coreBlk.Timestamp(),
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
		case coreChildBlk.ID():
			return coreChildBlk, nil
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
		case bytes.Equal(b, coreChildBlk.Bytes()):
			return coreChildBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	postForkBlk, ok := parentBlk.(*postForkBlock)
	require.True(ok)

	_, err = postForkBlk.Options(context.Background())
	require.True(errors.Is(err, snowman.ErrNotOracle))

	// Build the child
	statelessChild, err := block.BuildOption(
		postForkBlk.ID(),
		coreChildBlk.Bytes(),
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

func TestOptionTimestampValidity(t *testing.T) {
	require := require.New(t)
	coreVM, _, proVM, coreGenBlk, db := initTestProposerVM(t, time.Time{}, 0) // enable ProBlks

	coreOracleBlkID := ids.GenerateTestID()
	coreOracleBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     coreOracleBlkID,
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			HeightV:    coreGenBlk.Height() + 1,
			TimestampV: coreGenBlk.Timestamp().Add(time.Second),
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{2},
				ParentV:    coreOracleBlkID,
				TimestampV: coreGenBlk.Timestamp().Add(time.Second),
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{3},
				ParentV:    coreOracleBlkID,
				TimestampV: coreGenBlk.Timestamp().Add(time.Second),
			},
		},
	}
	statelessBlock, err := block.BuildUnsigned(
		coreGenBlk.ID(),
		coreGenBlk.Timestamp(),
		0,
		coreOracleBlk.Bytes(),
	)
	require.NoError(err)

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreOracleBlk.ID():
			return coreOracleBlk, nil
		case coreOracleBlk.opts[0].ID():
			return coreOracleBlk.opts[0], nil
		case coreOracleBlk.opts[1].ID():
			return coreOracleBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreOracleBlk.Bytes()):
			return coreOracleBlk, nil
		case bytes.Equal(b, coreOracleBlk.opts[0].Bytes()):
			return coreOracleBlk.opts[0], nil
		case bytes.Equal(b, coreOracleBlk.opts[1].Bytes()):
			return coreOracleBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	statefulBlock, err := proVM.ParseBlock(context.Background(), statelessBlock.Bytes())
	require.NoError(err)

	err = statefulBlock.Verify(context.Background())
	require.NoError(err)

	statefulOracleBlock, ok := statefulBlock.(snowman.OracleBlock)
	require.True(ok)

	options, err := statefulOracleBlock.Options(context.Background())
	require.NoError(err)

	option := options[0]
	err = option.Verify(context.Background())
	require.NoError(err)

	err = statefulBlock.Accept(context.Background())
	require.NoError(err)

	coreVM.GetBlockF = func(context.Context, ids.ID) (snowman.Block, error) {
		t.Fatal("called GetBlock when unable to handle the error")
		return nil, nil
	}
	coreVM.ParseBlockF = func(context.Context, []byte) (snowman.Block, error) {
		t.Fatal("called ParseBlock when unable to handle the error")
		return nil, nil
	}

	expectedTime := coreGenBlk.Timestamp()
	if optionTime := option.Timestamp(); !optionTime.Equal(expectedTime) {
		t.Fatalf("wrong time returned expected %s got %s", expectedTime, optionTime)
	}

	err = option.Accept(context.Background())
	require.NoError(err)

	// Restart the node.
	ctx := proVM.ctx
	sk, err := bls.NewSecretKey()
	require.NoError(err)

	proVM, err = New(
		coreVM,
		time.Time{}, // fork is active
		0,           // minimum P-Chain height
		DefaultMinBlockDelay,
		time.Time{}, // bls signing allowed
		pTestCert,
		sk,
	)
	require.NoError(err)

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
		return coreOracleBlk.opts[0].ID(), nil
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreOracleBlk.ID():
			return coreOracleBlk, nil
		case coreOracleBlk.opts[0].ID():
			return coreOracleBlk.opts[0], nil
		case coreOracleBlk.opts[1].ID():
			return coreOracleBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreOracleBlk.Bytes()):
			return coreOracleBlk, nil
		case bytes.Equal(b, coreOracleBlk.opts[0].Bytes()):
			return coreOracleBlk.opts[0], nil
		case bytes.Equal(b, coreOracleBlk.opts[1].Bytes()):
			return coreOracleBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	err = proVM.Initialize(
		context.Background(),
		ctx,
		db,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	require.NoError(err)

	statefulOptionBlock, err := proVM.ParseBlock(context.Background(), option.Bytes())
	require.NoError(err)
	require.Equal(choices.Accepted, statefulOptionBlock.Status())

	coreVM.GetBlockF = func(context.Context, ids.ID) (snowman.Block, error) {
		t.Fatal("called GetBlock when unable to handle the error")
		return nil, nil
	}
	coreVM.ParseBlockF = func(context.Context, []byte) (snowman.Block, error) {
		t.Fatal("called ParseBlock when unable to handle the error")
		return nil, nil
	}

	require.Equal(expectedTime, statefulOptionBlock.Timestamp())
}
