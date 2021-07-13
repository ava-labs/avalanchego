// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

type TestOptionsBlock struct {
	snowman.TestBlock
	opts [2]snowman.Block
}

func (tob TestOptionsBlock) Options() ([2]snowman.Block, error) {
	return tob.opts, nil
}

// ProposerBlock Option interface tests section
func TestOracle_PostForkOption_DoesNotImplementInterface(t *testing.T) {
	// setup
	proOpt := postForkOption{
		postForkCommonComponents: postForkCommonComponents{
			innerBlk: &TestOptionsBlock{},
		},
	}

	// test
	_, err := proOpt.Options()
	if err != snowman.ErrNotOracle {
		t.Fatal("PostForkOptions are not oracle blocks themselves")
	}
}

// ProposerBlock.Verify tests section
func TestBlockVerify_PostForkOption_ParentChecks(t *testing.T) {
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Time{})
	proVM.Set(coreGenBlk.Timestamp())

	// create post fork oracle block ...
	oracleCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(1111),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk,
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
			ParentV:    oracleCoreBlk,
			TimestampV: oracleCoreBlk.Timestamp(),
		},
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(3333),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{3},
			ParentV:    oracleCoreBlk,
			TimestampV: oracleCoreBlk.Timestamp(),
		},
	}

	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return oracleCoreBlk, nil }
	coreVM.CantGetBlock = true
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
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
			return nil, fmt.Errorf("Unknown block")
		}
	}

	parentBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	// retrieve options ...
	postForkOracleBlk, ok := parentBlk.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}
	opts, err := postForkOracleBlk.Options()
	if err != nil {
		t.Fatal("could not retrieve options from post fork oracle block")
	}
	if _, ok := opts[0].(*postForkOption); !ok {
		t.Fatal("unexpected option type")
	}

	// ... and verify them
	if err := opts[0].Verify(); err != nil {
		t.Fatal("option 0 should verify")
	}
	if err := opts[1].Verify(); err != nil {
		t.Fatal("option 1 should verify")
	}

	// show we can build on options
	if err := proVM.SetPreference(opts[0].ID()); err != nil {
		t.Fatal("could not set preference")
	}

	childCoreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(4444),
			StatusV: choices.Processing,
		},
		ParentV:    oracleCoreBlk.opts[0],
		BytesV:     []byte{4},
		TimestampV: oracleCoreBlk.opts[0].Timestamp().Add(proposer.MaxDelay),
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return childCoreBlk, nil }
	proVM.Set(childCoreBlk.Timestamp())

	proChild, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build on top of option")
	}
	if _, ok := proChild.(*postForkBlock); !ok {
		t.Fatal("unexpected block type")
	}
	if err := proChild.Verify(); err != nil {
		t.Fatal("block built on option does not verify")
	}
}

// ProposerBlock.Accept tests section
func TestBlockAccept_PostForkOption_SetsLastAcceptedBlock(t *testing.T) {
	// setup
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Time{})
	proVM.Set(coreGenBlk.Timestamp())

	// create post fork oracle block ...
	oracleCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(1111),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk,
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
			ParentV:    oracleCoreBlk,
			TimestampV: oracleCoreBlk.Timestamp(),
		},
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(3333),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{3},
			ParentV:    oracleCoreBlk,
			TimestampV: oracleCoreBlk.Timestamp(),
		},
	}

	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return oracleCoreBlk, nil }
	coreVM.CantGetBlock = true
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
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
			return nil, fmt.Errorf("Unknown block")
		}
	}

	parentBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	// accept oracle block
	if err := parentBlk.Accept(); err != nil {
		t.Fatal("could not accept block")
	}

	coreVM.LastAcceptedF = func() (ids.ID, error) {
		if oracleCoreBlk.Status() == choices.Accepted {
			return oracleCoreBlk.ID(), nil
		}
		return coreGenBlk.ID(), nil
	}
	if acceptedID, err := proVM.LastAccepted(); err != nil {
		t.Fatal("could not retrieve last accepted block")
	} else if acceptedID != parentBlk.ID() {
		t.Fatal("unexpected last accepted ID")
	}

	// accept one of the options
	postForkOracleBlk, ok := parentBlk.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}
	opts, err := postForkOracleBlk.Options()
	if err != nil {
		t.Fatal("could not retrieve options from post fork oracle block")
	}

	if err := opts[0].Accept(); err != nil {
		t.Fatal("could not accept option")
	}

	coreVM.LastAcceptedF = func() (ids.ID, error) {
		if oracleCoreBlk.opts[0].Status() == choices.Accepted {
			return oracleCoreBlk.opts[0].ID(), nil
		}
		return oracleCoreBlk.ID(), nil
	}
	if acceptedID, err := proVM.LastAccepted(); err != nil {
		t.Fatal("could not retrieve last accepted block")
	} else if acceptedID != opts[0].ID() {
		t.Fatal("unexpected last accepted ID")
	}
}

// ProposerBlock.Reject tests section
func TestBlockReject_InnerBlockIsNotRejected(t *testing.T) {
	// setup
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Time{})
	proVM.Set(coreGenBlk.Timestamp())

	// create post fork oracle block ...
	oracleCoreBlk := &TestOptionsBlock{
		TestBlock: snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(1111),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{1},
			ParentV:    coreGenBlk,
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
			ParentV:    oracleCoreBlk,
			TimestampV: oracleCoreBlk.Timestamp(),
		},
		&snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(3333),
				StatusV: choices.Processing,
			},
			BytesV:     []byte{3},
			ParentV:    oracleCoreBlk,
			TimestampV: oracleCoreBlk.Timestamp(),
		},
	}

	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return oracleCoreBlk, nil }
	coreVM.CantGetBlock = true
	coreVM.GetBlockF = func(blkID ids.ID) (snowman.Block, error) {
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
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
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
			return nil, fmt.Errorf("Unknown block")
		}
	}

	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build post fork oracle block")
	}

	// reject oracle block
	if err := builtBlk.Reject(); err != nil {
		t.Fatal("could not reject block")
	}
	proBlk, ok := builtBlk.(*postForkBlock)
	if !ok {
		t.Fatal("built block has not expected type")
	}

	if proBlk.Status() != choices.Rejected {
		t.Fatal("block rejection did not set state properly")
	}

	if proBlk.innerBlk.Status() == choices.Rejected {
		t.Fatal("block rejection unduly changed inner block status")
	}

	// reject an option
	postForkOracleBlk, ok := builtBlk.(*postForkBlock)
	if !ok {
		t.Fatal("expected post fork block")
	}
	opts, err := postForkOracleBlk.Options()
	if err != nil {
		t.Fatal("could not retrieve options from post fork oracle block")
	}

	if err := opts[0].Reject(); err != nil {
		t.Fatal("could not accept option")
	}
	proOpt, ok := opts[0].(*postForkOption)
	if !ok {
		t.Fatal("built block has not expected type")
	}

	if proOpt.Status() != choices.Rejected {
		t.Fatal("block rejection did not set state properly")
	}

	if proOpt.innerBlk.Status() == choices.Rejected {
		t.Fatal("block rejection unduly changed inner block status")
	}
}
