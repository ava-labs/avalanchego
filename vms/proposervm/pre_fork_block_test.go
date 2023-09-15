// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/mocks"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

func TestOracle_PreForkBlkImplementsInterface(t *testing.T) {
	require := require.New(t)

	// setup
	proBlk := preForkBlock{
		Block: &snowman.TestBlock{},
	}

	// test
	_, err := proBlk.Options(context.Background())
	require.Equal(snowman.ErrNotOracle, err)

	// setup
	proBlk = preForkBlock{
		Block: &TestOptionsBlock{},
	}

	// test
	_, err = proBlk.Options(context.Background())
	require.NoError(err)
}

func TestOracle_PreForkBlkCanBuiltOnPreForkOption(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, mockable.MaxTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create pre fork oracle block ...
	oracleCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(1111),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{1},
			ParentV: coreGenBlk.ID(),
		},
	}
	oracleCoreBlk.opts = [2]snowman.Block{
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(2222),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{2},
			ParentV: oracleCoreBlk.ID(),
		},
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(3333),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{3},
			ParentV: oracleCoreBlk.ID(),
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

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// retrieve options ...
	require.IsType(&preForkBlock{}, parentBlk)
	preForkOracleBlk := parentBlk.(*preForkBlock)
	opts, err := preForkOracleBlk.Options(context.Background())
	require.NoError(err)
	require.NoError(opts[0].Verify(context.Background()))

	// ... show a block can be built on top of an option
	require.NoError(proVM.SetPreference(context.Background(), opts[0].ID()))

	lastCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(4444),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{4},
			ParentV: oracleCoreBlk.opts[0].ID(),
		},
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return lastCoreBlk, nil
	}

	preForkChild, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, preForkChild)
}

func TestOracle_PostForkBlkCanBuiltOnPreForkOption(t *testing.T) {
	require := require.New(t)

	activationTime := genesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create pre fork oracle block pre activation time...
	oracleCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(1111),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: activationTime.Add(-1 * time.Second),
		},
	}

	// ... whose options are post activation time
	oracleCoreBlk.opts = [2]snowman.Block{
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(2222),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{2},
			ParentV:    oracleCoreBlk.ID(),
			TimestampV: activationTime.Add(time.Second),
		},
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(3333),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{3},
			ParentV:    oracleCoreBlk.ID(),
			TimestampV: activationTime.Add(time.Second),
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

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// retrieve options ...
	require.IsType(&preForkBlock{}, parentBlk)
	preForkOracleBlk := parentBlk.(*preForkBlock)
	opts, err := preForkOracleBlk.Options(context.Background())
	require.NoError(err)
	require.NoError(opts[0].Verify(context.Background()))

	// ... show a block can be built on top of an option
	require.NoError(proVM.SetPreference(context.Background(), opts[0].ID()))

	lastCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(4444),
				StatusV: choices.Processing,
			},
			BytesV:  []byte{4},
			ParentV: oracleCoreBlk.opts[0].ID(),
		},
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return lastCoreBlk, nil
	}

	postForkChild, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, postForkChild)
}

func TestBlockVerify_PreFork_ParentChecks(t *testing.T) {
	require := require.New(t)

	activationTime := genesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	require.True(coreGenBlk.Timestamp().Before(activationTime))

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
			return nil, database.ErrNotFound
		}
	}

	proVM.Set(proVM.Time().Add(proposer.MaxDelay))
	prntProBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// .. create child block ...
	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{2},
		TimestampV: prntCoreBlk.Timestamp().Add(proposer.MaxDelay),
	}
	childProBlk := preForkBlock{
		Block: childCoreBlk,
		vm:    proVM,
	}

	// child block referring unknown parent does not verify
	childCoreBlk.ParentV = ids.Empty
	err = childProBlk.Verify(context.Background())
	require.ErrorIs(err, database.ErrNotFound)

	// child block referring known parent does verify
	childCoreBlk.ParentV = prntProBlk.ID()
	require.NoError(childProBlk.Verify(context.Background()))
}

func TestBlockVerify_BlocksBuiltOnPreForkGenesis(t *testing.T) {
	require := require.New(t)

	activationTime := genesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	require.True(coreGenBlk.Timestamp().Before(activationTime))
	preActivationTime := activationTime.Add(-1 * time.Second)
	proVM.Set(preActivationTime)

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: preActivationTime,
		VerifyV:    nil,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	// preFork block verifies if parent is before fork activation time
	preForkChild, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, preForkChild)

	require.NoError(preForkChild.Verify(context.Background()))

	// postFork block does NOT verify if parent is before fork activation time
	postForkStatelessChild, err := block.Build(
		coreGenBlk.ID(),
		coreBlk.Timestamp(),
		0, // pChainHeight
		proVM.stakingCertLeaf,
		coreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.stakingLeafSigner,
	)
	require.NoError(err)
	postForkChild := &postForkBlock{
		SignedBlock: postForkStatelessChild,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: coreBlk,
			status:   choices.Processing,
		},
	}

	require.True(postForkChild.Timestamp().Before(activationTime))
	err = postForkChild.Verify(context.Background())
	require.ErrorIs(err, errProposersNotActivated)

	// once activation time is crossed postForkBlock are produced
	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreVM.SetPreferenceF = func(_ context.Context, id ids.ID) error {
		return nil
	}
	require.NoError(proVM.SetPreference(context.Background(), preForkChild.ID()))

	secondCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(2222),
		},
		BytesV:     []byte{2},
		ParentV:    coreBlk.ID(),
		TimestampV: postActivationTime,
		VerifyV:    nil,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return secondCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		switch id {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			require.FailNow("attempt to get unknown block")
			return nil, nil
		}
	}

	lastPreForkBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, lastPreForkBlk)

	require.NoError(lastPreForkBlk.Verify(context.Background()))

	require.NoError(proVM.SetPreference(context.Background(), lastPreForkBlk.ID()))
	thirdCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(333),
		},
		BytesV:     []byte{3},
		ParentV:    secondCoreBlk.ID(),
		TimestampV: postActivationTime,
		VerifyV:    nil,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return thirdCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, id ids.ID) (snowman.Block, error) {
		switch id {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		case secondCoreBlk.ID():
			return secondCoreBlk, nil
		default:
			require.FailNow("attempt to get unknown block")
			return nil, nil
		}
	}

	firstPostForkBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, firstPostForkBlk)

	require.NoError(firstPostForkBlk.Verify(context.Background()))
}

func TestBlockVerify_BlocksBuiltOnPostForkGenesis(t *testing.T) {
	require := require.New(t)

	activationTime := genesisTimestamp.Add(-1 * time.Second)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, 0)
	proVM.Set(activationTime)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// build parent block after fork activation time ...
	coreBlock := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:     []byte{1},
		ParentV:    coreGenBlk.ID(),
		TimestampV: coreGenBlk.Timestamp(),
		VerifyV:    nil,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlock, nil
	}

	// postFork block verifies if parent is after fork activation time
	postForkChild, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, postForkChild)

	require.NoError(postForkChild.Verify(context.Background()))

	// preFork block does NOT verify if parent is after fork activation time
	preForkChild := preForkBlock{
		Block: coreBlock,
		vm:    proVM,
	}
	err = preForkChild.Verify(context.Background())
	require.ErrorIs(err, errUnexpectedBlockType)
}

func TestBlockAccept_PreFork_SetsLastAcceptedBlock(t *testing.T) {
	require := require.New(t)

	// setup
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, mockable.MaxTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk.ID(),
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
	require.NoError(builtBlk.Accept(context.Background()))

	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		if coreBlk.Status() == choices.Accepted {
			return coreBlk.ID(), nil
		}
		return coreGenBlk.ID(), nil
	}
	acceptedID, err := proVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(builtBlk.ID(), acceptedID)
}

// ProposerBlock.Reject tests section
func TestBlockReject_PreForkBlock_InnerBlockIsRejected(t *testing.T) {
	require := require.New(t)

	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, mockable.MaxTime, 0) // disable ProBlks
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk.ID(),
		HeightV: coreGenBlk.Height() + 1,
	}
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}

	sb, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, sb)
	proBlk := sb.(*preForkBlock)

	require.NoError(proBlk.Reject(context.Background()))
	require.Equal(choices.Rejected, proBlk.Status())
	require.Equal(choices.Rejected, proBlk.Block.Status())
}

func TestBlockVerify_ForkBlockIsOracleBlock(t *testing.T) {
	require := require.New(t)

	activationTime := genesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	require.True(coreGenBlk.Timestamp().Before(activationTime))
	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreBlkID := ids.GenerateTestID()
	coreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     coreBlkID,
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: postActivationTime,
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{2},
				ParentV:    coreBlkID,
				TimestampV: postActivationTime,
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{3},
				ParentV:    coreBlkID,
				TimestampV: postActivationTime,
			},
		},
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		case coreBlk.opts[0].ID():
			return coreBlk.opts[0], nil
		case coreBlk.opts[1].ID():
			return coreBlk.opts[1], nil
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
		case bytes.Equal(b, coreBlk.opts[0].Bytes()):
			return coreBlk.opts[0], nil
		case bytes.Equal(b, coreBlk.opts[1].Bytes()):
			return coreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	firstBlock, err := proVM.ParseBlock(context.Background(), coreBlk.Bytes())
	require.NoError(err)

	require.NoError(firstBlock.Verify(context.Background()))

	oracleBlock, ok := firstBlock.(snowman.OracleBlock)
	require.True(ok)

	options, err := oracleBlock.Options(context.Background())
	require.NoError(err)

	require.NoError(options[0].Verify(context.Background()))

	require.NoError(options[1].Verify(context.Background()))
}

func TestBlockVerify_ForkBlockIsOracleBlockButChildrenAreSigned(t *testing.T) {
	require := require.New(t)

	activationTime := genesisTimestamp.Add(10 * time.Second)
	coreVM, _, proVM, coreGenBlk, _ := initTestProposerVM(t, activationTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	require.True(coreGenBlk.Timestamp().Before(activationTime))
	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreBlkID := ids.GenerateTestID()
	coreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     coreBlkID,
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk.ID(),
			TimestampV: postActivationTime,
		},
		opts: [2]snowman.Block{
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{2},
				ParentV:    coreBlkID,
				TimestampV: postActivationTime,
			},
			&snowman.TestBlock{
				TestDecidable: choices.TestDecidable{
					IDV:     ids.GenerateTestID(),
					StatusV: choices.Processing,
				},
				BytesV:     []byte{3},
				ParentV:    coreBlkID,
				TimestampV: postActivationTime,
			},
		},
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case coreGenBlk.ID():
			return coreGenBlk, nil
		case coreBlk.ID():
			return coreBlk, nil
		case coreBlk.opts[0].ID():
			return coreBlk.opts[0], nil
		case coreBlk.opts[1].ID():
			return coreBlk.opts[1], nil
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
		case bytes.Equal(b, coreBlk.opts[0].Bytes()):
			return coreBlk.opts[0], nil
		case bytes.Equal(b, coreBlk.opts[1].Bytes()):
			return coreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	firstBlock, err := proVM.ParseBlock(context.Background(), coreBlk.Bytes())
	require.NoError(err)

	require.NoError(firstBlock.Verify(context.Background()))

	slb, err := block.Build(
		firstBlock.ID(), // refer unknown parent
		firstBlock.Timestamp(),
		0, // pChainHeight,
		proVM.stakingCertLeaf,
		coreBlk.opts[0].Bytes(),
		proVM.ctx.ChainID,
		proVM.stakingLeafSigner,
	)
	require.NoError(err)

	invalidChild, err := proVM.ParseBlock(context.Background(), slb.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify(context.Background())
	require.ErrorIs(err, errUnexpectedBlockType)
}

// Assert that when the underlying VM implements ChainVMWithBuildBlockContext
// and the proposervm is activated, we only call the VM's BuildBlockWithContext
// when a P-chain height can be correctly provided from the parent block.
func TestPreForkBlock_BuildBlockWithContext(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	pChainHeight := uint64(1337)
	blkID := ids.GenerateTestID()
	innerBlk := snowman.NewMockBlock(ctrl)
	innerBlk.EXPECT().ID().Return(blkID).AnyTimes()
	innerBlk.EXPECT().Timestamp().Return(mockable.MaxTime)
	builtBlk := snowman.NewMockBlock(ctrl)
	builtBlk.EXPECT().Bytes().Return([]byte{1, 2, 3}).AnyTimes()
	builtBlk.EXPECT().ID().Return(ids.GenerateTestID()).AnyTimes()
	builtBlk.EXPECT().Height().Return(pChainHeight).AnyTimes()
	innerVM := mocks.NewMockChainVM(ctrl)
	innerVM.EXPECT().BuildBlock(gomock.Any()).Return(builtBlk, nil).AnyTimes()
	vdrState := validators.NewMockState(ctrl)
	vdrState.EXPECT().GetMinimumHeight(context.Background()).Return(pChainHeight, nil).AnyTimes()

	vm := &VM{
		ChainVM: innerVM,
		ctx: &snow.Context{
			ValidatorState: vdrState,
			Log:            logging.NoLog{},
		},
	}

	blk := &preForkBlock{
		Block: innerBlk,
		vm:    vm,
	}

	// Should call BuildBlock since proposervm won't have a P-chain height
	gotChild, err := blk.buildChild(context.Background())
	require.NoError(err)
	require.Equal(builtBlk, gotChild.(*postForkBlock).innerBlk)

	// Should call BuildBlock since proposervm is not activated
	innerBlk.EXPECT().Timestamp().Return(time.Time{})
	vm.activationTime = mockable.MaxTime

	gotChild, err = blk.buildChild(context.Background())
	require.NoError(err)
	require.Equal(builtBlk, gotChild.(*preForkBlock).Block)
}
