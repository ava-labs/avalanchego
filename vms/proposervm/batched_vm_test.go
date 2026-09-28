// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"crypto"
	"encoding/binary"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/proposervm/state"

	blockbuilder "github.com/ava-labs/avalanchego/vms/proposervm/block"
)

func TestCoreVMNotRemote(t *testing.T) {
	// if coreVM is not remote VM and no post-fork blocks can be served, a
	// specific error is returned
	require := require.New(t)
	_, _, proVM, _ := initTestProposerVM(t, upgradetest.Latest, 0)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	blkID := ids.Empty
	maxBlocksNum := 1000               // a high value to get all built blocks
	maxBlocksSize := 1000000           // a high value to get all built blocks
	maxBlocksRetrivalTime := time.Hour // a high value to get all built blocks
	_, err := proVM.GetAncestors(
		t.Context(),
		blkID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrivalTime,
	)
	require.ErrorIs(err, block.ErrRemoteVMNotImplemented)

	var blks [][]byte
	shouldBeEmpty, err := proVM.BatchedParseBlock(t.Context(), blks)
	require.NoError(err)
	require.Empty(shouldBeEmpty)
}

func TestGetAncestorsPreForkOnly(t *testing.T) {
	require := require.New(t)
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, upgradetest.NoUpgrades)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(t.Context()))
	}()

	// Build some prefork blocks....
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk1.ID()))
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
	builtBlk2, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk2.ID()))
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
	builtBlk3, err := proRemoteVM.BuildBlock(t.Context())
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
		t.Context(),
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
		t.Context(),
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
		t.Context(),
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
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, upgradetest.Latest)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(t.Context()))
	}()

	// Build some post-Fork blocks....
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)

	// prepare build of next block
	require.NoError(builtBlk1.Verify(t.Context()))
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk1.ID()))
	require.NoError(proRemoteVM.waitForProposerWindow())

	coreBlk2 := snowmantest.BuildChild(coreBlk1)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)

	// prepare build of next block
	require.NoError(builtBlk2.Verify(t.Context()))
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk2.ID()))
	require.NoError(proRemoteVM.waitForProposerWindow())

	coreBlk3 := snowmantest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)

	require.NoError(builtBlk3.Verify(t.Context()))
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk3.ID()))

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
		t.Context(),
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
		t.Context(),
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
		t.Context(),
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
	)

	// enable ProBlks in next future
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, upgradetest.Latest, forkTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(t.Context()))
	}()

	// Build some prefork blocks....
	proRemoteVM.Set(preForkTime)
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreBlk1.TimestampV = preForkTime
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk1)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk1.ID()))
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
	builtBlk2, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk2)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk2.ID()))
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
	builtBlk3, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk3)

	// prepare build of next block
	require.NoError(builtBlk3.Verify(t.Context()))
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk3.ID()))
	require.NoError(proRemoteVM.waitForProposerWindow())

	coreBlk4 := snowmantest.BuildChild(coreBlk3)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk4, nil
	}
	builtBlk4, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk4)
	require.NoError(builtBlk4.Verify(t.Context()))

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
		t.Context(),
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
		t.Context(),
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
		t.Context(),
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
		t.Context(),
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
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, upgradetest.NoUpgrades)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(t.Context()))
	}()

	// Build some prefork blocks....
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk1.ID()))
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
	builtBlk2, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk2.ID()))
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
	builtBlk3, err := proRemoteVM.BuildBlock(t.Context())
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
	res, err := proRemoteVM.BatchedParseBlock(t.Context(), bytesToParse)
	require.NoError(err)
	require.Len(res, 3)
	require.Equal(builtBlk1.ID(), res[0].ID())
	require.Equal(builtBlk2.ID(), res[1].ID())
	require.Equal(builtBlk3.ID(), res[2].ID())
}

func TestBatchedParseBlockParallel(t *testing.T) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	chainID := ids.GenerateTestID()

	vm := VM{
		ctx: &snow.Context{ChainID: chainID},
		ChainVM: &blocktest.VM{
			ParseBlockF: func(_ context.Context, rawBlock []byte) (snowman.Block, error) {
				return &snowmantest.Block{BytesV: rawBlock}, nil
			},
		},
	}

	tlsCert, err := staking.NewTLSCert()
	require.NoError(t, err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(t, err)
	key := tlsCert.PrivateKey.(crypto.Signer)

	blockThatCantBeParsed := snowmantest.BuildChild(snowmantest.Genesis)

	blocksWithUnparsable := makeParseableBlocks(t, parentID, timestamp, pChainHeight, cert, chainID, key)
	blocksWithUnparsable[50] = blockThatCantBeParsed.Bytes()

	parsableBlocks := makeParseableBlocks(t, parentID, timestamp, pChainHeight, cert, chainID, key)

	for _, testCase := range []struct {
		name         string
		preForkIndex int
		rawBlocks    [][]byte
	}{
		{
			name:      "empty input",
			rawBlocks: [][]byte{},
		},
		{
			name:         "pre-fork is somewhere in the middle",
			rawBlocks:    blocksWithUnparsable,
			preForkIndex: 50,
		},
		{
			name:         "all blocks are post fork",
			rawBlocks:    parsableBlocks,
			preForkIndex: len(parsableBlocks),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require := require.New(t)
			blocks, err := vm.BatchedParseBlock(t.Context(), testCase.rawBlocks)
			require.NoError(err)

			returnedBlockBytes := make([][]byte, len(blocks))
			for i, block := range blocks {
				returnedBlockBytes[i] = block.Bytes()
			}
			require.Equal(testCase.rawBlocks, returnedBlockBytes)

			for i, block := range blocks {
				if i < testCase.preForkIndex {
					require.IsType(&postForkBlock{}, block)
				} else {
					require.IsType(&preForkBlock{}, block)
				}
			}
		})
	}
}

func makeParseableBlocks(t *testing.T, parentID ids.ID, timestamp time.Time, pChainHeight uint64, cert *staking.Certificate, chainID ids.ID, key crypto.Signer) [][]byte {
	makeSignedBlock := func(i int) []byte {
		buff := binary.AppendVarint(nil, int64(i))

		signedBlock, err := blockbuilder.Build(
			parentID,
			timestamp,
			pChainHeight,
			blockbuilder.Epoch{},
			cert,
			buff,
			chainID,
			key,
		)
		require.NoError(t, err)

		return signedBlock.Bytes()
	}

	blockBytes := make([][]byte, 100)
	for i := range blockBytes {
		blockBytes[i] = makeSignedBlock(i)
	}
	return blockBytes
}

func TestBatchedParseBlockPostForkOnly(t *testing.T) {
	require := require.New(t)
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, upgradetest.Latest)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(t.Context()))
	}()

	// Build some post-Fork blocks....
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)

	// prepare build of next block
	require.NoError(builtBlk1.Verify(t.Context()))
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk1.ID()))
	require.NoError(proRemoteVM.waitForProposerWindow())

	coreBlk2 := snowmantest.BuildChild(coreBlk1)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)

	// prepare build of next block
	require.NoError(builtBlk2.Verify(t.Context()))
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk2.ID()))
	require.NoError(proRemoteVM.waitForProposerWindow())

	coreBlk3 := snowmantest.BuildChild(coreBlk2)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(t.Context())
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
	res, err := proRemoteVM.BatchedParseBlock(t.Context(), bytesToParse)
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
	)

	// enable ProBlks in next future
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, upgradetest.Latest, forkTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(t.Context()))
	}()

	// Build some prefork blocks....
	proRemoteVM.Set(preForkTime)
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreBlk1.TimestampV = preForkTime
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk1)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk1.ID()))
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
	builtBlk2, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk2)

	// prepare build of next block
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk2.ID()))
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
	builtBlk3, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk3)

	// prepare build of next block
	require.NoError(builtBlk3.Verify(t.Context()))
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk3.ID()))
	require.NoError(proRemoteVM.waitForProposerWindow())

	coreBlk4 := snowmantest.BuildChild(coreBlk3)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk4, nil
	}
	builtBlk4, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk4)
	require.NoError(builtBlk4.Verify(t.Context()))

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

	res, err := proRemoteVM.BatchedParseBlock(t.Context(), bytesToParse)
	require.NoError(err)
	require.Len(res, 4)
	require.Equal(builtBlk4.ID(), res[0].ID())
	require.Equal(builtBlk3.ID(), res[1].ID())
	require.Equal(builtBlk2.ID(), res[2].ID())
	require.Equal(builtBlk1.ID(), res[3].ID())
}

type TestRemoteProposerVM struct {
	*blocktest.BatchedVM
	*blocktest.VM
}

// initTestRemoteProposerVM creates a proposerVM for testing.
// If forkActivationTime is provided, the fork activates at that specific time.
// If not provided, the fork is already activated at InitiallyActiveTime.
func initTestRemoteProposerVM(
	t *testing.T,
	fork upgradetest.Fork,
	forkActivationTime ...time.Time,
) (
	TestRemoteProposerVM,
	*VM,
) {
	require := require.New(t)

	initialState := []byte("genesis state")
	coreVM := TestRemoteProposerVM{
		VM:        &blocktest.VM{},
		BatchedVM: &blocktest.BatchedVM{},
	}
	coreVM.VM.T = t
	coreVM.BatchedVM.T = t

	coreVM.InitializeF = func(
		context.Context,
		*snow.Context,
		database.Database,
		[]byte,
		[]byte,
		[]byte,
		[]*common.Fx,
		common.AppSender,
	) error {
		return nil
	}
	coreVM.LastAcceptedF = snowmantest.MakeLastAcceptedBlockF(
		[]*snowmantest.Block{snowmantest.Genesis},
	)
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

	var upgrades upgrade.Config
	if len(forkActivationTime) > 0 {
		upgrades = upgradetest.GetConfigWithUpgradeTime(fork, forkActivationTime[0])
	} else {
		upgrades = upgradetest.GetConfig(fork)
	}

	proVM := New(
		coreVM,
		Config{
			Upgrades:            upgrades,
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)

	valState := &validatorstest.State{
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
		t.Context(),
		ctx,
		prefixdb.New([]byte{}, memdb.New()), // make sure that DBs are compressed correctly
		initialState,
		nil,
		nil,
		nil,
		nil,
	))

	// Initialize shouldn't be called again
	coreVM.InitializeF = nil

	require.NoError(proVM.SetState(t.Context(), snow.NormalOp))
	require.NoError(proVM.SetPreference(t.Context(), snowmantest.GenesisID))
	return coreVM, proVM
}

// acceptedChainVM is a proposervm wrapping an inner VM that doesn't implement
// [block.BatchedChainVM]. The fork is activated at genesis, so every block
// built on it is post-fork.
type acceptedChainVM struct {
	tb     testing.TB
	coreVM *blocktest.VM
	proVM  *VM

	// innerBlocks and proBlocks hold every block built so far, indexed by
	// height. proBlocks[0] is nil because genesis is pre-fork.
	innerBlocks []*snowmantest.Block
	proBlocks   []snowman.Block

	innerBlocksByID    map[ids.ID]*snowmantest.Block
	innerBlocksByBytes map[string]*snowmantest.Block
	lastAcceptedHeight uint64
}

func newAcceptedChainVM(tb testing.TB, db database.Database, numHistoricalBlocks uint64) *acceptedChainVM {
	require := require.New(tb)

	c := &acceptedChainVM{
		tb:          tb,
		innerBlocks: []*snowmantest.Block{snowmantest.Genesis},
		proBlocks:   []snowman.Block{nil},
		innerBlocksByID: map[ids.ID]*snowmantest.Block{
			snowmantest.GenesisID: snowmantest.Genesis,
		},
		innerBlocksByBytes: map[string]*snowmantest.Block{
			string(snowmantest.GenesisBytes): snowmantest.Genesis,
		},
	}
	c.coreVM = &blocktest.VM{
		VM: enginetest.VM{
			InitializeF: func(context.Context, *snow.Context, database.Database, []byte, []byte, []byte, []*common.Fx, common.AppSender) error {
				return nil
			},
		},
		LastAcceptedF: func(context.Context) (ids.ID, error) {
			return c.innerBlocks[c.lastAcceptedHeight].ID(), nil
		},
		GetBlockF: func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
			if blk, ok := c.innerBlocksByID[blkID]; ok {
				return blk, nil
			}
			return nil, errUnknownBlock
		},
		ParseBlockF: func(_ context.Context, b []byte) (snowman.Block, error) {
			if blk, ok := c.innerBlocksByBytes[string(b)]; ok {
				return blk, nil
			}
			return nil, errUnknownBlock
		},
		GetBlockIDAtHeightF: func(_ context.Context, height uint64) (ids.ID, error) {
			if height > c.lastAcceptedHeight {
				return ids.Empty, errTooHigh
			}
			return c.innerBlocks[height].ID(), nil
		},
	}
	if t, ok := tb.(*testing.T); ok {
		c.coreVM.T = t
	}

	ctx := snowtest.Context(tb, snowtest.CChainID)
	ctx.NodeID = ids.NodeIDFromCert(pTestCert)
	ctx.ValidatorState = &validatorstest.State{
		GetMinimumHeightF: func(context.Context) (uint64, error) {
			return snowmantest.GenesisHeight, nil
		},
		GetCurrentHeightF: func(context.Context) (uint64, error) {
			return defaultPChainHeight, nil
		},
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return nil, nil
		},
	}

	c.proVM = New(
		c.coreVM,
		Config{
			Upgrades:            upgradetest.GetConfigWithUpgradeTime(upgradetest.ApricotPhase4, time.Time{}),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: numHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          prometheus.NewRegistry(),
		},
	)
	require.NoError(c.proVM.Initialize(
		tb.Context(),
		ctx,
		db,
		[]byte("genesis state"),
		nil,
		nil,
		nil,
		nil,
	))

	lastAcceptedID, err := c.proVM.LastAccepted(tb.Context())
	require.NoError(err)
	require.NoError(c.proVM.SetState(tb.Context(), snow.NormalOp))
	require.NoError(c.proVM.SetPreference(tb.Context(), lastAcceptedID))
	return c
}

// buildBlock builds and verifies a block on top of the most recently built
// block, and returns it.
func (c *acceptedChainVM) buildBlock() snowman.Block {
	require := require.New(c.tb)

	innerBlock := snowmantest.BuildChild(c.innerBlocks[len(c.innerBlocks)-1])
	c.coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return innerBlock, nil
	}
	proBlock, err := c.proVM.BuildBlock(c.tb.Context())
	require.NoError(err)
	require.NoError(proBlock.Verify(c.tb.Context()))
	require.NoError(c.proVM.SetPreference(c.tb.Context(), proBlock.ID()))

	c.innerBlocks = append(c.innerBlocks, innerBlock)
	c.proBlocks = append(c.proBlocks, proBlock)
	c.innerBlocksByID[innerBlock.ID()] = innerBlock
	c.innerBlocksByBytes[string(innerBlock.Bytes())] = innerBlock
	return proBlock
}

// acceptNextBlock accepts the lowest block that hasn't been accepted yet.
func (c *acceptedChainVM) acceptNextBlock() {
	height := c.lastAcceptedHeight + 1
	require.NoError(c.tb, c.proBlocks[height].Accept(c.tb.Context()))
	c.lastAcceptedHeight = height
}

// expectedAncestors returns the bytes of the blocks from height maxHeight down
// to height minHeight.
func (c *acceptedChainVM) expectedAncestors(maxHeight, minHeight uint64) [][]byte {
	expected := make([][]byte, 0, maxHeight-minHeight+1)
	for height := maxHeight; height >= minHeight; height-- {
		expected = append(expected, c.proBlocks[height].Bytes())
	}
	return expected
}

func TestGetAncestorsAcceptedWithoutBatchedInnerVM(t *testing.T) {
	const (
		numAccepted = 5
		numVerified = 2
		numBlocks   = numAccepted + numVerified

		// high values to get all built blocks
		maxBlocksNum           = 1000
		maxBlocksSize          = 1000000
		maxBlocksRetrievalTime = time.Hour
	)
	require := require.New(t)
	c := newAcceptedChainVM(t, memdb.New(), DefaultNumHistoricalBlocks)

	for range numAccepted {
		c.buildBlock()
		c.acceptNextBlock()
	}
	for range numVerified {
		c.buildBlock()
	}

	blockSize := func(height uint64) int {
		return wrappers.IntLen + len(c.proBlocks[height].Bytes())
	}

	tests := []struct {
		name                   string
		blkID                  ids.ID
		maxBlocksNum           int
		maxBlocksSize          int
		maxBlocksRetrievalTime time.Duration
		expected               [][]byte
		expectedErr            error
	}{
		{
			name:                   "verified and accepted blocks",
			blkID:                  c.proBlocks[numBlocks].ID(),
			maxBlocksNum:           maxBlocksNum,
			maxBlocksSize:          maxBlocksSize,
			maxBlocksRetrievalTime: maxBlocksRetrievalTime,
			expected:               c.expectedAncestors(numBlocks, 1),
		},
		{
			name:                   "accepted blocks",
			blkID:                  c.proBlocks[3].ID(),
			maxBlocksNum:           maxBlocksNum,
			maxBlocksSize:          maxBlocksSize,
			maxBlocksRetrievalTime: maxBlocksRetrievalTime,
			expected:               c.expectedAncestors(3, 1),
		},
		{
			name:                   "first post-fork block",
			blkID:                  c.proBlocks[1].ID(),
			maxBlocksNum:           maxBlocksNum,
			maxBlocksSize:          maxBlocksSize,
			maxBlocksRetrievalTime: maxBlocksRetrievalTime,
			expected:               c.expectedAncestors(1, 1),
		},
		{
			name:                   "max blocks num reached by verified blocks",
			blkID:                  c.proBlocks[numBlocks].ID(),
			maxBlocksNum:           1,
			maxBlocksSize:          maxBlocksSize,
			maxBlocksRetrievalTime: maxBlocksRetrievalTime,
			expected:               c.expectedAncestors(numBlocks, numBlocks),
		},
		{
			name:                   "max blocks num reached by accepted blocks",
			blkID:                  c.proBlocks[numBlocks].ID(),
			maxBlocksNum:           4,
			maxBlocksSize:          maxBlocksSize,
			maxBlocksRetrievalTime: maxBlocksRetrievalTime,
			expected:               c.expectedAncestors(numBlocks, numBlocks-3),
		},
		{
			name:                   "max blocks num reached at the first post-fork block",
			blkID:                  c.proBlocks[numAccepted].ID(),
			maxBlocksNum:           numAccepted,
			maxBlocksSize:          maxBlocksSize,
			maxBlocksRetrievalTime: maxBlocksRetrievalTime,
			expected:               c.expectedAncestors(numAccepted, 1),
		},
		{
			name:                   "max blocks size fits two accepted blocks",
			blkID:                  c.proBlocks[numAccepted].ID(),
			maxBlocksNum:           maxBlocksNum,
			maxBlocksSize:          blockSize(numAccepted) + blockSize(numAccepted-1) + 1,
			maxBlocksRetrievalTime: maxBlocksRetrievalTime,
			expected:               c.expectedAncestors(numAccepted, numAccepted-1),
		},
		{
			name:                   "max blocks size smaller than the requested block",
			blkID:                  c.proBlocks[numAccepted].ID(),
			maxBlocksNum:           maxBlocksNum,
			maxBlocksSize:          1,
			maxBlocksRetrievalTime: maxBlocksRetrievalTime,
			expected:               c.expectedAncestors(numAccepted, numAccepted),
		},
		{
			name:                   "no retrieval time still serves the requested block",
			blkID:                  c.proBlocks[numAccepted].ID(),
			maxBlocksNum:           maxBlocksNum,
			maxBlocksSize:          maxBlocksSize,
			maxBlocksRetrievalTime: 0,
			expected:               c.expectedAncestors(numAccepted, numAccepted),
		},
		{
			name:                   "pre-fork block",
			blkID:                  snowmantest.GenesisID,
			maxBlocksNum:           maxBlocksNum,
			maxBlocksSize:          maxBlocksSize,
			maxBlocksRetrievalTime: maxBlocksRetrievalTime,
			expectedErr:            block.ErrRemoteVMNotImplemented,
		},
		{
			name:                   "unknown block",
			blkID:                  ids.GenerateTestID(),
			maxBlocksNum:           maxBlocksNum,
			maxBlocksSize:          maxBlocksSize,
			maxBlocksRetrievalTime: maxBlocksRetrievalTime,
			expectedErr:            block.ErrRemoteVMNotImplemented,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res, err := c.proVM.GetAncestors(
				t.Context(),
				test.blkID,
				test.maxBlocksNum,
				test.maxBlocksSize,
				test.maxBlocksRetrievalTime,
			)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, res)
		})
	}
}

// TestGetAncestorsPrunedHistory verifies that a request is served up to the
// oldest retained block when historical blocks are pruned.
func TestGetAncestorsPrunedHistory(t *testing.T) {
	const (
		numHistoricalBlocks = 2
		numAccepted         = 5
		// The last accepted block isn't considered historical, so the
		// blocks at heights below this one are pruned.
		oldestRetainedHeight = numAccepted - numHistoricalBlocks

		maxBlocksNum           = 1000
		maxBlocksSize          = 1000000
		maxBlocksRetrievalTime = time.Hour
	)
	require := require.New(t)
	c := newAcceptedChainVM(t, memdb.New(), numHistoricalBlocks)

	for range numAccepted {
		c.buildBlock()
		c.acceptNextBlock()
	}

	_, err := c.proVM.State.GetBlockIDAtHeight(oldestRetainedHeight - 1)
	require.ErrorIs(err, database.ErrNotFound)

	res, err := c.proVM.GetAncestors(
		t.Context(),
		c.proBlocks[numAccepted].ID(),
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrievalTime,
	)
	require.NoError(err)
	require.Equal(c.expectedAncestors(numAccepted, oldestRetainedHeight), res)

	res, err = c.proVM.GetAncestors(
		t.Context(),
		c.proBlocks[oldestRetainedHeight].ID(),
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrievalTime,
	)
	require.NoError(err)
	require.Equal(c.expectedAncestors(oldestRetainedHeight, oldestRetainedHeight), res)

	// A pruned block can't be served by the proposervm.
	_, err = c.proVM.GetAncestors(
		t.Context(),
		c.proBlocks[oldestRetainedHeight-1].ID(),
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrievalTime,
	)
	require.ErrorIs(err, block.ErrRemoteVMNotImplemented)
}

// TestGetAncestorsMultipleReadBatches verifies that a response spanning
// several concurrent read batches is served in order and that the limits are
// enforced within later batches.
func TestGetAncestorsMultipleReadBatches(t *testing.T) {
	const (
		numAccepted = 3*initialAncestorsReadBatchSize + 5

		maxBlocksNum           = 1000
		maxBlocksSize          = 1000000
		maxBlocksRetrievalTime = time.Hour
	)
	require := require.New(t)
	c := newAcceptedChainVM(t, memdb.New(), DefaultNumHistoricalBlocks)

	for range numAccepted {
		c.buildBlock()
		c.acceptNextBlock()
	}
	tipID := c.proBlocks[numAccepted].ID()

	// sizeOfTopBlocks returns the response size of the num highest blocks.
	sizeOfTopBlocks := func(num uint64) int {
		size := 0
		for height := uint64(numAccepted); height > numAccepted-num; height-- {
			size += wrappers.IntLen + len(c.proBlocks[height].Bytes())
		}
		return size
	}

	tests := []struct {
		name          string
		maxBlocksNum  int
		maxBlocksSize int
		expected      [][]byte
	}{
		{
			name:          "all blocks",
			maxBlocksNum:  maxBlocksNum,
			maxBlocksSize: maxBlocksSize,
			expected:      c.expectedAncestors(numAccepted, 1),
		},
		{
			name:          "max blocks num reached in a later batch",
			maxBlocksNum:  initialAncestorsReadBatchSize + 7,
			maxBlocksSize: maxBlocksSize,
			expected:      c.expectedAncestors(numAccepted, numAccepted-initialAncestorsReadBatchSize-6),
		},
		{
			name:          "max blocks size reached in a later batch",
			maxBlocksNum:  maxBlocksNum,
			maxBlocksSize: sizeOfTopBlocks(initialAncestorsReadBatchSize+3) + 1,
			expected:      c.expectedAncestors(numAccepted, numAccepted-initialAncestorsReadBatchSize-2),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res, err := c.proVM.GetAncestors(
				t.Context(),
				tipID,
				test.maxBlocksNum,
				test.maxBlocksSize,
				maxBlocksRetrievalTime,
			)
			require.NoError(err)
			require.Equal(test.expected, res)
		})
	}
}

// TestGetAncestorsIndexedBlockNotStored verifies that a block that is indexed
// by height but isn't stored ends the served run of blocks.
func TestGetAncestorsIndexedBlockNotStored(t *testing.T) {
	const (
		numAccepted   = 5
		missingHeight = 3

		maxBlocksNum           = 1000
		maxBlocksSize          = 1000000
		maxBlocksRetrievalTime = time.Hour
	)
	require := require.New(t)
	c := newAcceptedChainVM(t, memdb.New(), DefaultNumHistoricalBlocks)

	for range numAccepted {
		c.buildBlock()
		c.acceptNextBlock()
	}

	missingID := c.proBlocks[missingHeight].ID()
	require.NoError(c.proVM.State.DeleteBlock(missingID))

	// The height index still references the block.
	indexedID, err := c.proVM.State.GetBlockIDAtHeight(missingHeight)
	require.NoError(err)
	require.Equal(missingID, indexedID)

	res, err := c.proVM.GetAncestors(
		t.Context(),
		c.proBlocks[numAccepted].ID(),
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrievalTime,
	)
	require.NoError(err)
	require.Equal(c.expectedAncestors(numAccepted, missingHeight+1), res)

	_, err = c.proVM.GetAncestors(
		t.Context(),
		missingID,
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrievalTime,
	)
	require.ErrorIs(err, block.ErrRemoteVMNotImplemented)

	res, err = c.proVM.GetAncestors(
		t.Context(),
		c.proBlocks[missingHeight-1].ID(),
		maxBlocksNum,
		maxBlocksSize,
		maxBlocksRetrievalTime,
	)
	require.NoError(err)
	require.Equal(c.expectedAncestors(missingHeight-1, 1), res)
}

// TestGetBlocksBytesTimedOut verifies that the concurrent reads stop once the
// time limit is reported as reached and that only a prefix of the requested
// blocks is returned.
func TestGetBlocksBytesTimedOut(t *testing.T) {
	const numAccepted = 5

	require := require.New(t)
	c := newAcceptedChainVM(t, memdb.New(), DefaultNumHistoricalBlocks)

	for range numAccepted {
		c.buildBlock()
		c.acceptNextBlock()
	}

	blkIDs := make([]ids.ID, 0, numAccepted)
	expected := make([][]byte, 0, numAccepted)
	for height := uint64(numAccepted); height > 0; height-- {
		blkIDs = append(blkIDs, c.proBlocks[height].ID())
		expected = append(expected, c.proBlocks[height].Bytes())
	}

	never := func() bool { return false }
	res, err := c.proVM.getBlocksBytes(blkIDs, never)
	require.NoError(err)
	require.Equal(expected, res)

	always := func() bool { return true }
	res, err = c.proVM.getBlocksBytes(blkIDs, always)
	require.NoError(err)
	require.Empty(res)

	// Timing out part way through leaves a prefix of the requested blocks,
	// whose length depends on how the reads were scheduled.
	var calls atomic.Int64
	afterTwoCalls := func() bool {
		return calls.Add(1) > 2
	}
	res, err = c.proVM.getBlocksBytes(blkIDs, afterTwoCalls)
	require.NoError(err)
	require.LessOrEqual(len(res), 2)
	require.Equal(expected[:len(res)], res)
}

// TestGetAncestorsHeightIndexMismatch verifies that a height index that
// disagrees with the stored block is reported rather than served.
func TestGetAncestorsHeightIndexMismatch(t *testing.T) {
	const numAccepted = 3

	require := require.New(t)
	c := newAcceptedChainVM(t, memdb.New(), DefaultNumHistoricalBlocks)

	for range numAccepted {
		c.buildBlock()
		c.acceptNextBlock()
	}

	require.NoError(c.proVM.State.SetBlockIDAtHeight(numAccepted, ids.GenerateTestID()))

	_, err := c.proVM.GetAncestors(
		t.Context(),
		c.proBlocks[numAccepted].ID(),
		1000,
		1000000,
		time.Hour,
	)
	require.ErrorIs(err, errUnexpectedBlockAtHeight)
}

// TestGetAncestorsAcceptedAtSnomanPlusPlusFork verifies that accepted post-fork
// blocks are served locally and that the pre-fork remainder of the request is
// delegated to the inner VM with the remaining limits.
func TestGetAncestorsAcceptedAtSnomanPlusPlusFork(t *testing.T) {
	require := require.New(t)

	var (
		currentTime  = time.Now().Truncate(time.Second)
		preForkTime  = currentTime.Add(5 * time.Minute)
		forkTime     = currentTime.Add(10 * time.Minute)
		postForkTime = currentTime.Add(15 * time.Minute)
	)

	// enable ProBlks in next future
	coreVM, proRemoteVM := initTestRemoteProposerVM(t, upgradetest.Latest, forkTime)
	defer func() {
		require.NoError(proRemoteVM.Shutdown(t.Context()))
	}()

	// Build and accept some prefork blocks....
	proRemoteVM.Set(preForkTime)
	coreBlk1 := snowmantest.BuildChild(snowmantest.Genesis)
	coreBlk1.TimestampV = preForkTime
	coreBlk2 := snowmantest.BuildChild(coreBlk1)
	coreBlk2.TimestampV = postForkTime
	coreBlk3 := snowmantest.BuildChild(coreBlk2)
	coreBlk4 := snowmantest.BuildChild(coreBlk3)
	coreBlks := []*snowmantest.Block{coreBlk1, coreBlk2, coreBlk3, coreBlk4}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		if blkID == snowmantest.GenesisID {
			return snowmantest.Genesis, nil
		}
		for _, blk := range coreBlks {
			if blk.ID() == blkID {
				return blk, nil
			}
		}
		return nil, errUnknownBlock
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		if bytes.Equal(b, snowmantest.GenesisBytes) {
			return snowmantest.Genesis, nil
		}
		for _, blk := range coreBlks {
			if bytes.Equal(b, blk.Bytes()) {
				return blk, nil
			}
		}
		return nil, errUnknownBlock
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk1, nil
	}
	builtBlk1, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk1)
	require.NoError(builtBlk1.Verify(t.Context()))
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk1.ID()))
	require.NoError(builtBlk1.Accept(t.Context()))

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk2, nil
	}
	builtBlk2, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&preForkBlock{}, builtBlk2)
	require.NoError(builtBlk2.Verify(t.Context()))
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk2.ID()))
	require.NoError(builtBlk2.Accept(t.Context()))

	// .. and some post-fork
	proRemoteVM.Set(postForkTime)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk3, nil
	}
	builtBlk3, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk3)
	require.NoError(builtBlk3.Verify(t.Context()))
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk3.ID()))
	require.NoError(builtBlk3.Accept(t.Context()))
	require.NoError(proRemoteVM.waitForProposerWindow())

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk4, nil
	}
	builtBlk4, err := proRemoteVM.BuildBlock(t.Context())
	require.NoError(err)
	require.IsType(&postForkBlock{}, builtBlk4)
	require.NoError(builtBlk4.Verify(t.Context()))
	require.NoError(proRemoteVM.SetPreference(t.Context(), builtBlk4.ID()))
	require.NoError(builtBlk4.Accept(t.Context()))

	// ...Call GetAncestors on them ...
	type innerRequest struct {
		BlkID         ids.ID
		MaxBlocksNum  int
		MaxBlocksSize int
	}
	var innerRequests []innerRequest
	coreVM.GetAncestorsF = func(_ context.Context, blkID ids.ID, maxBlocksNum, maxBlocksSize int, _ time.Duration) ([][]byte, error) {
		innerRequests = append(innerRequests, innerRequest{
			BlkID:         blkID,
			MaxBlocksNum:  maxBlocksNum,
			MaxBlocksSize: maxBlocksSize,
		})

		sortedBlocks := [][]byte{
			coreBlk2.Bytes(),
			coreBlk1.Bytes(),
		}
		var startIndex int
		switch blkID {
		case coreBlk2.ID():
			startIndex = 0
		case coreBlk1.ID():
			startIndex = 1
		default:
			return nil, nil // unknown blockID
		}

		endIndex := min(startIndex+maxBlocksNum, len(sortedBlocks))
		return sortedBlocks[startIndex:endIndex], nil
	}

	const (
		maxBlocksNum  = 1000    // a high value to get all built blocks
		maxBlocksSize = 1000000 // a high value to get all built blocks
	)
	postForkSize := 2*wrappers.IntLen + len(builtBlk4.Bytes()) + len(builtBlk3.Bytes())

	// load all known blocks
	res, err := proRemoteVM.GetAncestors(
		t.Context(),
		builtBlk4.ID(),
		maxBlocksNum,
		maxBlocksSize,
		10*time.Minute,
	)
	require.NoError(err)
	require.Equal([][]byte{
		builtBlk4.Bytes(),
		builtBlk3.Bytes(),
		builtBlk2.Bytes(),
		builtBlk1.Bytes(),
	}, res)
	require.Equal([]innerRequest{{
		BlkID:         coreBlk2.ID(),
		MaxBlocksNum:  maxBlocksNum - 2,
		MaxBlocksSize: maxBlocksSize - postForkSize,
	}}, innerRequests)

	// load some post-fork and some pre-fork blocks
	innerRequests = nil
	res, err = proRemoteVM.GetAncestors(
		t.Context(),
		builtBlk4.ID(),
		3,
		maxBlocksSize,
		10*time.Minute,
	)
	require.NoError(err)
	require.Equal([][]byte{
		builtBlk4.Bytes(),
		builtBlk3.Bytes(),
		builtBlk2.Bytes(),
	}, res)
	require.Equal([]innerRequest{{
		BlkID:         coreBlk2.ID(),
		MaxBlocksNum:  1,
		MaxBlocksSize: maxBlocksSize - postForkSize,
	}}, innerRequests)

	// the max number of blocks is reached by post-fork blocks, so the inner
	// VM isn't asked
	innerRequests = nil
	res, err = proRemoteVM.GetAncestors(
		t.Context(),
		builtBlk4.ID(),
		2,
		maxBlocksSize,
		10*time.Minute,
	)
	require.NoError(err)
	require.Equal([][]byte{
		builtBlk4.Bytes(),
		builtBlk3.Bytes(),
	}, res)
	require.Empty(innerRequests)

	// a pre-fork block is entirely served by the inner VM
	innerRequests = nil
	res, err = proRemoteVM.GetAncestors(
		t.Context(),
		builtBlk1.ID(),
		maxBlocksNum,
		maxBlocksSize,
		10*time.Minute,
	)
	require.NoError(err)
	require.Equal([][]byte{builtBlk1.Bytes()}, res)
	require.Equal([]innerRequest{{
		BlkID:         coreBlk1.ID(),
		MaxBlocksNum:  maxBlocksNum,
		MaxBlocksSize: maxBlocksSize,
	}}, innerRequests)
}

// BenchmarkGetAncestors compares serving a request through [VM.GetAncestors]
// against the [block.GetAncestors] fallback of repeated [VM.GetBlock] calls,
// which is what the consensus engine used for the primary network chains when
// the inner VM didn't implement [block.BatchedChainVM].
func BenchmarkGetAncestors(b *testing.B) {
	// defaultAncestorsMaxBlockCount is the default maximum number of blocks
	// requested by GetAncestors.
	const defaultAncestorsMaxBlockCount = 2000

	require := require.New(b)

	// Use a real database implementation to make the benchmark more
	// realistic.
	db, err := pebbledb.New(b.TempDir(), nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(err)
	b.Cleanup(func() {
		require.NoError(db.Close())
	})

	c := newAcceptedChainVM(b, db, DefaultNumHistoricalBlocks)
	for range defaultAncestorsMaxBlockCount {
		c.buildBlock()
		c.acceptNextBlock()
	}
	tipID := c.proBlocks[defaultAncestorsMaxBlockCount].ID()

	type serialGetter struct{ block.Getter }
	for _, bench := range []struct {
		name string
		vm   block.Getter
	}{
		{"batched", c.proVM},
		{"serial", serialGetter{c.proVM}},
	} {
		b.Run(bench.name, func(b *testing.B) {
			for b.Loop() {
				// Reset the block caches so that every iteration reads from
				// the database, as a request for historical blocks would.
				c.proVM.State = state.New(c.proVM.db)

				res, err := block.GetAncestors(
					b.Context(),
					logging.NoLog{},
					bench.vm,
					tipID,
					defaultAncestorsMaxBlockCount,
					constants.MaxContainersLen,
					time.Minute,
				)
				require.NoError(err)
				require.Len(res, defaultAncestorsMaxBlockCount)
			}
		})
	}
}
