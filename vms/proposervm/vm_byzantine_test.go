// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/vms/proposervm/block"
)

// Ensure that a byzantine node issuing an invalid PreForkBlock (Y) when the
// parent block (X) is issued into a PostForkBlock (A) will be marked as invalid
// correctly.
//
//	    G
//	  / |
//	A - X
//	    |
//	    Y
func TestInvalidByzantineProposerParent(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	xBlock := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}

	aBlock, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	coreVM.BuildBlockF = nil

	require.NoError(aBlock.Verify(context.Background()))
	require.NoError(aBlock.Accept(context.Background()))

	yBlock := snowmantest.BuildChild(xBlock)
	coreVM.ParseBlockF = func(_ context.Context, blockBytes []byte) (snowman.Block, error) {
		if !bytes.Equal(blockBytes, yBlock.Bytes()) {
			return nil, errUnknownBlock
		}
		return yBlock, nil
	}

	parsedBlock, err := proVM.ParseBlock(context.Background(), yBlock.Bytes())
	if err != nil {
		// If there was an error parsing, then this is fine.
		return
	}

	// If there wasn't an error parsing - verify must return an error
	err = parsedBlock.Verify(context.Background())
	require.ErrorIs(err, errUnknownBlock)
}

// Ensure that a byzantine node issuing an invalid PreForkBlock (Y or Z) when
// the parent block (X) is issued into a PostForkBlock (A) will be marked as
// invalid correctly.
//
//	    G
//	  / |
//	A - X
//	   / \
//	  Y   Z
func TestInvalidByzantineProposerOracleParent(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	proVM.Set(snowmantest.GenesisTimestamp)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	xTestBlock := snowmantest.BuildChild(snowmantest.Genesis)
	xBlock := &TestOptionsBlock{
		Block: *xTestBlock,
		opts: [2]*snowmantest.Block{
			snowmantest.BuildChild(xTestBlock),
			snowmantest.BuildChild(xTestBlock),
		},
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case xBlock.ID():
			return xBlock, nil
		case xBlock.opts[0].ID():
			return xBlock.opts[0], nil
		case xBlock.opts[1].ID():
			return xBlock.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, xBlock.Bytes()):
			return xBlock, nil
		case bytes.Equal(b, xBlock.opts[0].Bytes()):
			return xBlock.opts[0], nil
		case bytes.Equal(b, xBlock.opts[1].Bytes()):
			return xBlock.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	aBlockIntf, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.IsType(&postForkBlock{}, aBlockIntf)
	aBlock := aBlockIntf.(*postForkBlock)
	opts, err := aBlock.Options(context.Background())
	require.NoError(err)

	require.NoError(aBlock.Verify(context.Background()))
	require.NoError(opts[0].Verify(context.Background()))
	require.NoError(opts[1].Verify(context.Background()))

	wrappedXBlock, err := proVM.ParseBlock(context.Background(), xBlock.Bytes())
	require.NoError(err)

	// This should never be invoked by the consensus engine. However, it is
	// enforced to fail verification as a failsafe.
	err = wrappedXBlock.Verify(context.Background())
	require.ErrorIs(err, errUnexpectedBlockType)
}

// Ensure that a byzantine node issuing an invalid PostForkBlock (B) when the
// parent block (X) is issued into a PostForkBlock (A) will be marked as invalid
// correctly.
//
//	    G
//	  / |
//	A - X
//	  / |
//	B - Y
func TestInvalidByzantineProposerPreForkParent(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	xBlock := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}

	yBlock := snowmantest.BuildChild(xBlock)
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case xBlock.ID():
			return xBlock, nil
		case yBlock.ID():
			return yBlock, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, blockBytes []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(blockBytes, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(blockBytes, xBlock.Bytes()):
			return xBlock, nil
		case bytes.Equal(blockBytes, yBlock.Bytes()):
			return yBlock, nil
		default:
			return nil, errUnknownBlock
		}
	}

	aBlock, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	coreVM.BuildBlockF = nil

	require.NoError(aBlock.Verify(context.Background()))

	wrappedXBlock, err := proVM.ParseBlock(context.Background(), xBlock.Bytes())
	require.NoError(err)

	// This should never be invoked by the consensus engine. However, it is
	// enforced to fail verification as a failsafe.
	err = wrappedXBlock.Verify(context.Background())
	require.ErrorIs(err, errUnexpectedBlockType)
}

// Ensure that a byzantine node issuing an invalid OptionBlock (B) which
// contains core block (Y) whose parent (G) doesn't match (B)'s parent (A)'s
// inner block (X) will be marked as invalid correctly.
//
//	    G
//	  / | \
//	A - X  |
//	|     /
//	B - Y
func TestBlockVerify_PostForkOption_FaultyParent(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	proVM.Set(snowmantest.GenesisTimestamp)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	xBlock := &TestOptionsBlock{
		Block: *snowmantest.BuildChild(snowmantest.Genesis),
		opts: [2]*snowmantest.Block{ // valid blocks should reference xBlock
			snowmantest.BuildChild(snowmantest.Genesis),
			snowmantest.BuildChild(snowmantest.Genesis),
		},
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case xBlock.ID():
			return xBlock, nil
		case xBlock.opts[0].ID():
			return xBlock.opts[0], nil
		case xBlock.opts[1].ID():
			return xBlock.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, xBlock.Bytes()):
			return xBlock, nil
		case bytes.Equal(b, xBlock.opts[0].Bytes()):
			return xBlock.opts[0], nil
		case bytes.Equal(b, xBlock.opts[1].Bytes()):
			return xBlock.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	aBlockIntf, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.IsType(&postForkBlock{}, aBlockIntf)
	aBlock := aBlockIntf.(*postForkBlock)
	opts, err := aBlock.Options(context.Background())
	require.NoError(err)

	require.NoError(aBlock.Verify(context.Background()))
	err = opts[0].Verify(context.Background())
	require.ErrorIs(err, errInnerParentMismatch)
	err = opts[1].Verify(context.Background())
	require.ErrorIs(err, errInnerParentMismatch)
}

//	  ,--G ----.
//	 /    \     \
//	A(X)  B(Y)  C(Z)
//	| \_ /_____/
//	|\  /   |
//	| \/    |
//	O2 O1   O3
//
// O1.parent = B (non-Oracle), O1.inner = first option of X (invalid)
// O2.parent = A (original), O2.inner = first option of X (valid)
// O3.parent = C (Oracle), O3.inner = first option of X (invalid parent)
func TestBlockVerify_InvalidPostForkOption(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	proVM.Set(snowmantest.GenesisTimestamp)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create an Oracle pre-fork block X
	xTestBlock := snowmantest.BuildChild(snowmantest.Genesis)
	xBlock := &TestOptionsBlock{
		Block: *xTestBlock,
		opts: [2]*snowmantest.Block{
			snowmantest.BuildChild(xTestBlock),
			snowmantest.BuildChild(xTestBlock),
		},
	}

	xInnerOptions, err := xBlock.Options(context.Background())
	require.NoError(err)
	xInnerOption := xInnerOptions[0]

	// create a non-Oracle pre-fork block Y
	yBlock := snowmantest.BuildChild(snowmantest.Genesis)
	ySlb, err := block.BuildUnsigned(
		snowmantest.GenesisID,
		snowmantest.GenesisTimestamp,
		uint64(2000),
		0,           // pChainEpochHeight
		0,           // epochNumber
		time.Time{}, // epochStartTime
		yBlock.Bytes(),
	)
	require.NoError(err)

	// create post-fork block B from Y
	bBlock := postForkBlock{
		SignedBlock: ySlb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: yBlock,
		},
	}

	require.NoError(bBlock.Verify(context.Background()))

	// generate O1
	statelessOuterOption, err := block.BuildOption(
		bBlock.ID(),
		xInnerOption.Bytes(),
	)
	require.NoError(err)

	outerOption := &postForkOption{
		Block: statelessOuterOption,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: xInnerOption,
		},
	}

	err = outerOption.Verify(context.Background())
	require.ErrorIs(err, errUnexpectedBlockType)

	// generate A from X and O2
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return xBlock, nil
	}
	aBlock, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	coreVM.BuildBlockF = nil
	require.NoError(aBlock.Verify(context.Background()))

	statelessOuterOption, err = block.BuildOption(
		aBlock.ID(),
		xInnerOption.Bytes(),
	)
	require.NoError(err)

	outerOption = &postForkOption{
		Block: statelessOuterOption,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: xInnerOption,
		},
	}

	require.NoError(outerOption.Verify(context.Background()))

	// create an Oracle pre-fork block Z
	// create post-fork block B from Y
	zTestBlock := snowmantest.BuildChild(snowmantest.Genesis)
	zBlock := &TestOptionsBlock{
		Block: *zTestBlock,
		opts: [2]*snowmantest.Block{
			snowmantest.BuildChild(zTestBlock),
			snowmantest.BuildChild(zTestBlock),
		},
	}

	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return zBlock, nil
	}
	cBlock, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	coreVM.BuildBlockF = nil
	require.NoError(cBlock.Verify(context.Background()))

	// generate O3
	statelessOuterOption, err = block.BuildOption(
		cBlock.ID(),
		xInnerOption.Bytes(),
	)
	require.NoError(err)

	outerOption = &postForkOption{
		Block: statelessOuterOption,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: xInnerOption,
		},
	}

	err = outerOption.Verify(context.Background())
	require.ErrorIs(err, errInnerParentMismatch)
}

func TestGetBlock_MutatedSignature(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, valState, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// Make sure that we will be sampled to perform the proposals.
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{
			proVM.ctx.NodeID: {
				NodeID: proVM.ctx.NodeID,
				Weight: 10,
			},
		}, nil
	}

	proVM.Set(snowmantest.GenesisTimestamp)

	// Create valid core blocks to build our chain on.
	coreBlk0 := snowmantest.BuildChild(snowmantest.Genesis)
	coreBlk1 := snowmantest.BuildChild(coreBlk0)
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		switch blkID {
		case snowmantest.GenesisID:
			return snowmantest.Genesis, nil
		case coreBlk0.ID():
			return coreBlk0, nil
		case coreBlk1.ID():
			return coreBlk1, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, snowmantest.GenesisBytes):
			return snowmantest.Genesis, nil
		case bytes.Equal(b, coreBlk0.Bytes()):
			return coreBlk0, nil
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		default:
			return nil, errUnknownBlock
		}
	}

	// Build the first proposal block
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk0, nil
	}

	builtBlk0, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(builtBlk0.Verify(context.Background()))

	require.NoError(proVM.SetPreference(context.Background(), builtBlk0.ID()))

	// The second proposal block will need to be signed because the timestamp
	// hasn't moved forward

	// Craft what would be the next block, but with an invalid signature:
	// ID: 2R3Uz98YmxHUJARWv6suApPdAbbZ7X7ipat1gZuZNNhC5wPwJW
	// Valid Bytes: 000000000000fd81ce4f1ab2650176d46a3d1fbb593af5717a2ada7dabdcef19622325a8ce8400000000000003e800000000000006d0000004a13082049d30820285a003020102020100300d06092a864886f70d01010b050030003020170d3939313233313030303030305a180f32313231313132333130313030305a300030820222300d06092a864886f70d01010105000382020f003082020a0282020100b9c3615c42d501f3b9d21ed127b31855827dbe12652e6e6f278991a3ad1ca55e2241b1cac69a0aeeefdd913db8ae445ff847789fdcbc1cbe6cce0a63109d1c1fb9d441c524a6eb1412f9b8090f1507e3e50a725f9d0a9d5db424ea229a7c11d8b91c73fecbad31c7b216bb2ac5e4d5ff080a80fabc73b34beb8fa46513ab59d489ce3f273c0edab43ded4d4914e081e6e850f9e502c3c4a54afc8a3a89d889aec275b7162a7616d53a61cd3ee466394212e5bef307790100142ad9e0b6c95ad2424c6e84d06411ad066d0c37d4d14125bae22b49ad2a761a09507bbfe43d023696d278d9fbbaf06c4ff677356113d3105e248078c33caed144d85929b1dd994df33c5d3445675104659ca9642c269b5cfa39c7bad5e399e7ebce3b5e6661f989d5f388006ebd90f0e035d533f5662cb925df8744f61289e66517b51b9a2f54792dca9078d5e12bf8ad79e35a68d4d661d15f0d3029d6c5903c845323d5426e49deaa2be2bc261423a9cd77df9a2706afaca27f589cc2c8f53e2a1f90eb5a3f8bcee0769971db6bacaec265d86b39380f69e3e0e06072de986feede26fe856c55e24e88ee5ac342653ac55a04e21b8517310c717dff0e22825c0944c6ba263f8f060099ea6e44a57721c7aa54e2790a4421fb85e3347e4572cba44e62b2cad19c1623c1cab4a715078e56458554cef8442769e6d5dd7f99a6234653a46828804f0203010001a320301e300e0603551d0f0101ff0404030204b0300c0603551d130101ff04023000300d06092a864886f70d01010b050003820201004ee2229d354720a751e2d2821134994f5679997113192626cf61594225cfdf51e6479e2c17e1013ab9dceb713bc0f24649e5cab463a8cf8617816ed736ac5251a853ff35e859ac6853ebb314f967ff7867c53512d42e329659375682c854ca9150cfa4c3964680e7650beb93e8b4a0d6489a9ca0ce0104752ba4d9cf3e2dc9436b56ecd0bd2e33cbbeb5a107ec4fd6f41a943c8bee06c0b32f4291a3e3759a7984d919a97d5d6517b841053df6e795ed33b52ed5e41357c3e431beb725e4e4f2ef956c44fd1f76fa4d847602e491c3585a90cdccfff982405d388b83d6f32ea16da2f5e4595926a7d26078e32992179032d30831b1f1b42de1781c507536a49adb4c95bad04c171911eed30d63c73712873d1e8094355efb9aeee0c16f8599575fd7f8bb027024bad63b097d2230d8f0ba12a8ed23e618adc3d7cb6a63e02b82a6d4d74b21928dbcb6d3788c6fd45022d69f3ab94d914d97cd651db662e92918a5d891ef730a813f03aade2fe385b61f44840f8925ad3345df1c82c9de882bb7184b4cd0bbd9db8322aaedb4ff86e5be9635987e6c40455ab9b063cdb423bee2edcac47cf654487e9286f33bdbad10018f4db9564cee6e048570e1517a2e396501b5978a53d10a548aed26938c2f9aada3ae62d3fdae486deb9413dffb6524666453633d665c3712d0fec9f844632b2b3eaf0267ca495eb41dba8273862609de00000001020000020098147a41989d8626f63d0966b39376143e45ea6e21b62761a115660d88db9cba37be71d1e1153e7546eb075749122449f2f3f5984e51773f082700d847334da35babe72a66e5a49c9a96cd763bdd94258263ae92d30da65d7c606482d0afe9f4f884f4f6c33d6d8e1c0c71061244ebec6a9dbb9b78bfbb71dec572aa0c0d8e532bf779457e05412b75acf12f35c75917a3eda302aaa27c3090e93bf5de0c3e30968cf8ba025b91962118bbdb6612bf682ba6e87ae6cd1a5034c89559b76af870395dc17ec592e9dbb185633aa1604f8d648f82142a2d1a4dabd91f816b34e73120a70d061e64e6da62ba434fd0cdf7296aa67fd5e0432ef8cee67c1b59aee91c99288c17a8511d96ba7339fb4ae5da453289aa7a9fab00d37035accae24eef0eaf517148e67bdc76adaac2429508d642df3033ad6c9e3fb53057244c1295f2ed3ac66731f77178fccb7cc4fd40778ccb061e5d53cd0669371d8d355a4a733078a9072835b5564a52a50f5db8525d2ee00466124a8d40d9959281b86a789bd0769f3fb0deb89f0eb9cfe036ff8a0011f52ca551c30202f46680acfa656ccf32a4e8a7121ef52442128409dc40d21d61205839170c7b022f573c2cfdaa362df22e708e7572b9b77f4fb20fe56b122bcb003566e20caef289f9d7992c2f1ad0c8366f71e8889390e0d14e2e76c56b515933b0c337ac6bfcf76d33e2ba50cb62eb71
	// Invalid Bytes: 000000000000fd81ce4f1ab2650176d46a3d1fbb593af5717a2ada7dabdcef19622325a8ce8400000000000003e800000000000006d0000004a13082049d30820285a003020102020100300d06092a864886f70d01010b050030003020170d3939313233313030303030305a180f32313231313132333130313030305a300030820222300d06092a864886f70d01010105000382020f003082020a0282020100b9c3615c42d501f3b9d21ed127b31855827dbe12652e6e6f278991a3ad1ca55e2241b1cac69a0aeeefdd913db8ae445ff847789fdcbc1cbe6cce0a63109d1c1fb9d441c524a6eb1412f9b8090f1507e3e50a725f9d0a9d5db424ea229a7c11d8b91c73fecbad31c7b216bb2ac5e4d5ff080a80fabc73b34beb8fa46513ab59d489ce3f273c0edab43ded4d4914e081e6e850f9e502c3c4a54afc8a3a89d889aec275b7162a7616d53a61cd3ee466394212e5bef307790100142ad9e0b6c95ad2424c6e84d06411ad066d0c37d4d14125bae22b49ad2a761a09507bbfe43d023696d278d9fbbaf06c4ff677356113d3105e248078c33caed144d85929b1dd994df33c5d3445675104659ca9642c269b5cfa39c7bad5e399e7ebce3b5e6661f989d5f388006ebd90f0e035d533f5662cb925df8744f61289e66517b51b9a2f54792dca9078d5e12bf8ad79e35a68d4d661d15f0d3029d6c5903c845323d5426e49deaa2be2bc261423a9cd77df9a2706afaca27f589cc2c8f53e2a1f90eb5a3f8bcee0769971db6bacaec265d86b39380f69e3e0e06072de986feede26fe856c55e24e88ee5ac342653ac55a04e21b8517310c717dff0e22825c0944c6ba263f8f060099ea6e44a57721c7aa54e2790a4421fb85e3347e4572cba44e62b2cad19c1623c1cab4a715078e56458554cef8442769e6d5dd7f99a6234653a46828804f0203010001a320301e300e0603551d0f0101ff0404030204b0300c0603551d130101ff04023000300d06092a864886f70d01010b050003820201004ee2229d354720a751e2d2821134994f5679997113192626cf61594225cfdf51e6479e2c17e1013ab9dceb713bc0f24649e5cab463a8cf8617816ed736ac5251a853ff35e859ac6853ebb314f967ff7867c53512d42e329659375682c854ca9150cfa4c3964680e7650beb93e8b4a0d6489a9ca0ce0104752ba4d9cf3e2dc9436b56ecd0bd2e33cbbeb5a107ec4fd6f41a943c8bee06c0b32f4291a3e3759a7984d919a97d5d6517b841053df6e795ed33b52ed5e41357c3e431beb725e4e4f2ef956c44fd1f76fa4d847602e491c3585a90cdccfff982405d388b83d6f32ea16da2f5e4595926a7d26078e32992179032d30831b1f1b42de1781c507536a49adb4c95bad04c171911eed30d63c73712873d1e8094355efb9aeee0c16f8599575fd7f8bb027024bad63b097d2230d8f0ba12a8ed23e618adc3d7cb6a63e02b82a6d4d74b21928dbcb6d3788c6fd45022d69f3ab94d914d97cd651db662e92918a5d891ef730a813f03aade2fe385b61f44840f8925ad3345df1c82c9de882bb7184b4cd0bbd9db8322aaedb4ff86e5be9635987e6c40455ab9b063cdb423bee2edcac47cf654487e9286f33bdbad10018f4db9564cee6e048570e1517a2e396501b5978a53d10a548aed26938c2f9aada3ae62d3fdae486deb9413dffb6524666453633d665c3712d0fec9f844632b2b3eaf0267ca495eb41dba8273862609de00000001020000000101
	invalidBlkBytesHex := "000000000000fd81ce4f1ab2650176d46a3d1fbb593af5717a2ada7dabdcef19622325a8ce8400000000000003e800000000000006d0000004a13082049d30820285a003020102020100300d06092a864886f70d01010b050030003020170d3939313233313030303030305a180f32313231313132333130313030305a300030820222300d06092a864886f70d01010105000382020f003082020a0282020100b9c3615c42d501f3b9d21ed127b31855827dbe12652e6e6f278991a3ad1ca55e2241b1cac69a0aeeefdd913db8ae445ff847789fdcbc1cbe6cce0a63109d1c1fb9d441c524a6eb1412f9b8090f1507e3e50a725f9d0a9d5db424ea229a7c11d8b91c73fecbad31c7b216bb2ac5e4d5ff080a80fabc73b34beb8fa46513ab59d489ce3f273c0edab43ded4d4914e081e6e850f9e502c3c4a54afc8a3a89d889aec275b7162a7616d53a61cd3ee466394212e5bef307790100142ad9e0b6c95ad2424c6e84d06411ad066d0c37d4d14125bae22b49ad2a761a09507bbfe43d023696d278d9fbbaf06c4ff677356113d3105e248078c33caed144d85929b1dd994df33c5d3445675104659ca9642c269b5cfa39c7bad5e399e7ebce3b5e6661f989d5f388006ebd90f0e035d533f5662cb925df8744f61289e66517b51b9a2f54792dca9078d5e12bf8ad79e35a68d4d661d15f0d3029d6c5903c845323d5426e49deaa2be2bc261423a9cd77df9a2706afaca27f589cc2c8f53e2a1f90eb5a3f8bcee0769971db6bacaec265d86b39380f69e3e0e06072de986feede26fe856c55e24e88ee5ac342653ac55a04e21b8517310c717dff0e22825c0944c6ba263f8f060099ea6e44a57721c7aa54e2790a4421fb85e3347e4572cba44e62b2cad19c1623c1cab4a715078e56458554cef8442769e6d5dd7f99a6234653a46828804f0203010001a320301e300e0603551d0f0101ff0404030204b0300c0603551d130101ff04023000300d06092a864886f70d01010b050003820201004ee2229d354720a751e2d2821134994f5679997113192626cf61594225cfdf51e6479e2c17e1013ab9dceb713bc0f24649e5cab463a8cf8617816ed736ac5251a853ff35e859ac6853ebb314f967ff7867c53512d42e329659375682c854ca9150cfa4c3964680e7650beb93e8b4a0d6489a9ca0ce0104752ba4d9cf3e2dc9436b56ecd0bd2e33cbbeb5a107ec4fd6f41a943c8bee06c0b32f4291a3e3759a7984d919a97d5d6517b841053df6e795ed33b52ed5e41357c3e431beb725e4e4f2ef956c44fd1f76fa4d847602e491c3585a90cdccfff982405d388b83d6f32ea16da2f5e4595926a7d26078e32992179032d30831b1f1b42de1781c507536a49adb4c95bad04c171911eed30d63c73712873d1e8094355efb9aeee0c16f8599575fd7f8bb027024bad63b097d2230d8f0ba12a8ed23e618adc3d7cb6a63e02b82a6d4d74b21928dbcb6d3788c6fd45022d69f3ab94d914d97cd651db662e92918a5d891ef730a813f03aade2fe385b61f44840f8925ad3345df1c82c9de882bb7184b4cd0bbd9db8322aaedb4ff86e5be9635987e6c40455ab9b063cdb423bee2edcac47cf654487e9286f33bdbad10018f4db9564cee6e048570e1517a2e396501b5978a53d10a548aed26938c2f9aada3ae62d3fdae486deb9413dffb6524666453633d665c3712d0fec9f844632b2b3eaf0267ca495eb41dba8273862609de00000001020000000101"
	invalidBlkBytes, err := hex.DecodeString(invalidBlkBytesHex)
	require.NoError(err)

	invalidBlk, err := proVM.ParseBlock(context.Background(), invalidBlkBytes)
	if err != nil {
		// Not being able to parse an invalid block is fine.
		t.Skip(err)
	}

	err = invalidBlk.Verify(context.Background())
	require.ErrorIs(err, database.ErrNotFound)

	// Note that the invalidBlk.ID() is the same as the correct blk ID because
	// the signature isn't part of the blk ID.
	blkID, err := ids.FromString("2R3Uz98YmxHUJARWv6suApPdAbbZ7X7ipat1gZuZNNhC5wPwJW")
	require.NoError(err)
	require.Equal(blkID, invalidBlk.ID())

	// GetBlock shouldn't really be able to succeed, as we don't have a valid
	// representation of [blkID]
	proVM.innerBlkCache.Flush() // So we don't get from the cache
	fetchedBlk, err := proVM.GetBlock(context.Background(), blkID)
	if err != nil {
		t.Skip(err)
	}

	// GetBlock returned, so it must have somehow gotten a valid representation
	// of [blkID].
	require.NoError(fetchedBlk.Verify(context.Background()))
}
