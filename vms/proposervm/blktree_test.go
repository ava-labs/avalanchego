package proposervm

import (
	"bytes"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func TestAccept_SingleBlock(t *testing.T) {
	// there is just one ProBlock with no siblings being accepted
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	// generate one proposer block...
	coreVM.CantBuildBlock = true
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2021),
			StatusV: choices.Processing,
		},
		ParentV: coreGenBlk,
		VerifyV: nil,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) { return coreBlk, nil }

	proBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk.Status() != choices.Processing {
		t.Fatal("Newly built pro blocks must have processing status")
	}

	// ..accept it and check that both it and its core block are accepted
	if err := proBlk.Accept(); err != nil {
		t.Fatal("Could not accept proposer block")
	}

	if proBlk.Status() != choices.Accepted {
		t.Fatal("accepted pro block has wrong status")
	}
	if coreBlk.Status() != choices.Accepted {
		t.Fatal("accepted core block has wrong status")
	}

	proVM.state.wipeCache() // check persistence
	if proBlk, err = proVM.GetBlock(proBlk.ID()); err != nil {
		t.Fatal("could not retrieve proBlk after cache wiping")
	}
	if proBlk.Status() != choices.Accepted {
		t.Fatal("accepted pro block has wrong status")
	}
	if coreBlk.Status() != choices.Accepted {
		t.Fatal("accepted core block has wrong status")
	}
}

func TestAccept_BlockConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap different coreBlocks and are siblings;
	// Once proBlk1 is accepted, proBlk2 is rejected
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	// generate proBlk1 and proBlk2 with different coreBlocks
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk1, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{2},
		ParentV: coreGenBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk1.Bytes()):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.Bytes()):
			return coreBlk2, nil
		default:
			t.Fatal("Parsing unknown core block")
			return nil, nil
		}
	}

	// ..accept proBlk2 and check that proBlk2 is accepted and proBlk1 rejected
	if err := proBlk2.Accept(); err != nil {
		t.Fatal("Could not accept proposer block")
	}

	if proBlk2.Status() != choices.Accepted {
		t.Fatal("Accepted pro block has wrong status")
	}
	if coreBlk2.Status() != choices.Accepted {
		t.Fatal("Accepted core block has wrong status")
	}

	if proBlk1.Status() != choices.Processing {
		t.Fatal("proposer block status update is not propagated")
	}
	if coreBlk1.Status() != choices.Rejected {
		t.Fatal("Rejected core block has wrong status")
	}

	proVM.state.wipeCache() // check persistence
	if proBlk2, err = proVM.GetBlock(proBlk2.ID()); err != nil {
		t.Fatal("could not retrieve proBlk after cache wiping")
	}
	if proBlk2.Status() != choices.Accepted {
		t.Fatal("Accepted pro block has wrong status")
	}
	if coreBlk2.Status() != choices.Accepted {
		t.Fatal("Accepted core block has wrong status")
	}

	if proBlk1, err = proVM.GetBlock(proBlk1.ID()); err != nil {
		t.Fatal("could not retrieve proBlk after cache wiping")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proposer block status update is not propagated")
	}
	if coreBlk1.Status() != choices.Rejected {
		t.Fatal("Rejected core block has wrong status")
	}
}

func TestAccept_ChainConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap different coreBlocks and are siblings; proBlk2 has a child proBlk3
	// Once proBlk1 is accepted, proBlk2 and proBlk3 are rejected
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	// generate proBlk1 and proBlk2,proBlk3 chain with different coreBlocks
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk1, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{2},
		ParentV: coreGenBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("could not set preference")
	}

	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{3},
		ParentV: coreBlk2,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
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
			t.Fatal("Parsing unknown core block")
			return nil, nil
		}
	}

	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	// ..accept proBlk1 and check that proBlk1 is accepted and proBlk2/proBlk3 are  rejected
	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not accept proposer block")
	}

	if proBlk1.Status() != choices.Accepted {
		t.Fatal("Accepted pro block has wrong status")
	}
	if coreBlk1.Status() != choices.Accepted {
		t.Fatal("Accepted core block has wrong status")
	}

	if proBlk2.Status() != choices.Processing {
		t.Fatal("proposer block status update is not propagated")
	}
	if coreBlk2.Status() != choices.Rejected {
		t.Fatal("Rejected core block has wrong status")
	}

	if proBlk3.Status() != choices.Processing {
		t.Fatal("proposer block status update is not propagated")
	}
	if coreBlk3.Status() != choices.Rejected {
		t.Fatal("Rejected core block has wrong status")
	}

	proVM.state.wipeCache() // check persistence
	if proBlk1, err = proVM.GetBlock(proBlk1.ID()); err != nil {
		t.Fatal("could not retrieve proBlk after cache wiping")
	}
	if proBlk1.Status() != choices.Accepted {
		t.Fatal("Accepted pro block has wrong status")
	}
	if coreBlk1.Status() != choices.Accepted {
		t.Fatal("Accepted core block has wrong status")
	}

	if proBlk2, err = proVM.GetBlock(proBlk2.ID()); err != nil {
		t.Fatal("could not retrieve proBlk after cache wiping")
	}
	if proBlk2.Status() != choices.Processing {
		t.Fatal("proposer block status update is not propagated")
	}
	if coreBlk2.Status() != choices.Rejected {
		t.Fatal("Rejected core block has wrong status")
	}

	if proBlk3, err = proVM.GetBlock(proBlk3.ID()); err != nil {
		t.Fatal("could not retrieve proBlk after cache wiping")
	}
	if proBlk3.Status() != choices.Processing {
		t.Fatal("proposer block status update is not propagated")
	}
	if coreBlk3.Status() != choices.Rejected {
		t.Fatal("Rejected core block has wrong status")
	}
}

func TestAccept_MixedCoreBlocksConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap the same coreBlocks coreBlk and are siblings; proBlk2 has a child proBlk3
	// Once proBlk1 is accepted, proBlk2 and proBlk3 are rejected, while coreBlk is accepted
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	pChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }
	nodeID, err := ids.ToShortID(hashing.PubkeyBytesToAddress(pTestCert.Leaf.Raw))
	if err != nil {
		t.Fatal("Could not evalute nodeID")
	}
	valVM.GetValidatorsF = func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
		res := make(map[ids.ShortID]uint64)
		res[nodeID] = uint64(10)
		return res, nil
	}

	// generate proBlk1 and proBlk2,proBlk3 chain with different coreBlocks
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	pChainHeight = uint64(4000) // move ahead P-Chain height so that proBlk1 != proBlk2
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("could not set preference")
	}

	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{3},
		ParentV: coreBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }
	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		case bytes.Equal(b, coreBlk3.Bytes()):
			return coreBlk3, nil
		default:
			t.Fatal("Parsing unknown core block")
			return nil, nil
		}
	}

	// ..accept proBlk1 and check that proBlk1 is accepted and proBlk2/proBlk3 are  rejected
	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not accept proposer block")
	}

	if proBlk1.Status() != choices.Accepted {
		t.Fatal("Accepted pro block has wrong status")
	}
	if coreBlk.Status() != choices.Accepted {
		t.Fatal("Accepted core block has wrong status")
	}

	if proBlk2.Status() != choices.Processing {
		t.Fatal("proposer block status update is not propagated")
	}

	if proBlk3.Status() != choices.Processing {
		t.Fatal("proposer block status update is not propagated")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("Child core block has wrong status")
	}

	proVM.state.wipeCache() // check persistence
	if proBlk1, err = proVM.GetBlock(proBlk1.ID()); err != nil {
		t.Fatal("could not retrieve proBlk after cache wiping")
	}
	if proBlk1.Status() != choices.Accepted {
		t.Fatal("Accepted pro block has wrong status")
	}
	if coreBlk.Status() != choices.Accepted {
		t.Fatal("Accepted core block has wrong status")
	}

	if proBlk2, err = proVM.GetBlock(proBlk2.ID()); err != nil {
		t.Fatal("could not retrieve proBlk after cache wiping")
	}
	if proBlk2.Status() != choices.Processing {
		t.Fatal("proposer block status update is not propagated")
	}

	if proBlk3, err = proVM.GetBlock(proBlk3.ID()); err != nil {
		t.Fatal("could not retrieve proBlk after cache wiping")
	}
	if proBlk3.Status() != choices.Processing {
		t.Fatal("proposer block status update is not propagated")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("Child core block has wrong status")
	}
}

// TODO: accept out-of-order test, just to document behavior. IF ACCEPT CAN COME CHILDREN FIRST

// ProposerBlock.Reject tests section
func TestReject_BlockConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap different coreBlocks and are siblings;
	// ProBlk2 is rejected but its core not; ProBlk1 and core blocks statues as set on proBlk1's Accept
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	// generate proBlk1 and proBlk2 with different coreBlocks
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk1, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{2},
		ParentV: coreGenBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	// ..reject proBlk2 and check that other blocks statuses are unaffected
	if err := proBlk2.Reject(); err != nil {
		t.Fatal("Could not reject proposer block")
	}

	if proBlk2.Status() != choices.Rejected {
		t.Fatal("Rejected pro block has wrong status")
	}
	if coreBlk2.Status() != choices.Processing {
		t.Fatal("Rejecting Pro block should not affect core block")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("Rejection of a sibling should not affect block status")
	}
	if coreBlk1.Status() != choices.Processing {
		t.Fatal("Rejection of a sibling should not affect core block status")
	}

	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not accept proposer block")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("Rejected pro block should stay rejected upon sibling's accept")
	}
	if coreBlk2.Status() != choices.Rejected {
		t.Fatal("this core block should be rejected one sibling is accepted")
	}
	if proBlk1.Status() != choices.Accepted {
		t.Fatal("problk1 should be accepted")
	}
	if coreBlk1.Status() != choices.Accepted {
		t.Fatal("coreblk1 should be accepted")
	}
}

func TestReject_ParentFirst_ChainConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap different coreBlocks and are siblings; proBlk2 has a child proBlk3
	// ProBlk2 and ProBlk3 are rejected in this order before ProBlk1 is accepted
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	// generate proBlk1 and proBlk2,proBlk3 chain with different coreBlocks
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk1, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{2},
		ParentV: coreGenBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("could not set preference")
	}

	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{3},
		ParentV: coreBlk2,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }
	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
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
			t.Fatal("Parsing unknown core block")
			return nil, nil
		}
	}

	// ..Reject ProBlk2 ...
	if err := proBlk2.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk2")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("proBlk2 should be Rejected")
	}
	if coreBlk2.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Processing {
		t.Fatal("proposer block status update is not propagated")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("coreBlk3 should not be rejected")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of sibling")
	}
	if coreBlk1.Status() != choices.Processing {
		t.Fatal("coreBlk1 should be unaffected by rejection of sibling")
	}

	//  ... and ProBlk3,...
	if err := proBlk3.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk3")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("ProBlk2 should stay rejected")
	}
	if coreBlk2.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should be rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of sibling")
	}
	if coreBlk1.Status() != choices.Processing {
		t.Fatal("coreBlk1 should be unaffected by rejection of sibling")
	}

	// ...finally accept ProBlk1
	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not reject ProBlk1")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("ProBlk2 should stay rejected")
	}
	if coreBlk2.Status() != choices.Rejected {
		t.Fatal("Acceptance of sibling should cause rejection of coreBlk2")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Rejected {
		t.Fatal("Acceptance of uncle should cause rejection of coreBlk2")
	}
	if proBlk1.Status() != choices.Accepted {
		t.Fatal("proBlk1 should be accepted")
	}
	if coreBlk1.Status() != choices.Accepted {
		t.Fatal("coreBlk1 should be accepted")
	}
}

func TestReject_ChildFirst_ChainConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap different coreBlocks and are siblings; proBlk2 has a child proBlk3
	// ProBlk3 and ProBlk2 are rejected in this order before ProBlk1 is accepted
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	// generate proBlk1 and proBlk2,proBlk3 chain with different coreBlocks
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk1, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(2222),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{2},
		ParentV: coreGenBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk2, nil }
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("could not set preference")
	}

	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{3},
		ParentV: coreBlk2,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }
	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
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
			t.Fatal("Parsing unknown core block")
			return nil, nil
		}
	}

	//  ... Reject ProBlk3 ...
	if err := proBlk3.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk3")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should be rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk2.Status() != choices.Processing {
		t.Fatal("ProBlk2 should still be processing")
	}
	if coreBlk2.Status() != choices.Processing {
		t.Fatal("coreBlk2 should still be processing")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of siblings' descendants")
	}
	if coreBlk1.Status() != choices.Processing {
		t.Fatal("coreBlk1 should be unaffected by rejection of siblings' descendants")
	}

	// ... and ProBlk2, ...
	if err := proBlk2.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk2")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("proBlk2 should be Rejected")
	}
	if coreBlk2.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("proBlk2 should stay Rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("coreBlk3 should still be processing")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of sibling")
	}
	if coreBlk1.Status() != choices.Processing {
		t.Fatal("coreBlk1 should be unaffected by rejection of sibling")
	}

	// ...finally accept ProBlk1
	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not reject ProBlk1")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("ProBlk2 should stay rejected")
	}
	if coreBlk2.Status() != choices.Rejected {
		t.Fatal("Acceptance of sibling should cause rejection of coreBlk2")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Rejected {
		t.Fatal("Acceptance of uncle should cause rejection of coreBlk3")
	}
	if proBlk1.Status() != choices.Accepted {
		t.Fatal("proBlk1 should be accepted")
	}
	if coreBlk1.Status() != choices.Accepted {
		t.Fatal("coreBlk1 should be accepted")
	}
}

func TestReject_ParentFirst_MixedCoreBlocksConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap the same coreBlocks coreBlk and are siblings; proBlk2 has a child proBlk3
	// proBlk2 and proBlk3 are rejected in this order, then proBlk1 is accepted
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	pChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }
	nodeID, err := ids.ToShortID(hashing.PubkeyBytesToAddress(pTestCert.Leaf.Raw))
	if err != nil {
		t.Fatal("Could not evalute nodeID")
	}
	valVM.GetValidatorsF = func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
		res := make(map[ids.ShortID]uint64)
		res[nodeID] = uint64(10)
		return res, nil
	}

	// generate proBlk1 and proBlk2,proBlk3 chain with different coreBlocks
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	pChainHeight = uint64(4000) // move ahead P-Chain height so that proBlk1 != proBlk2
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("could not set preference")
	}

	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{3},
		ParentV: coreBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }
	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		case bytes.Equal(b, coreBlk3.Bytes()):
			return coreBlk3, nil
		default:
			t.Fatal("Parsing unknown core block")
			return nil, nil
		}
	}

	// ..Reject ProBlk2 ...
	if err := proBlk2.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk2")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("proBlk2 should be Rejected")
	}
	if coreBlk.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Processing {
		t.Fatal("proposer block status update is not propagated")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("coreBlk3 should still be processing")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of sibling")
	}

	//  ... and ProBlk3,...
	if err := proBlk3.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk3")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("ProBlk2 should stay rejected")
	}
	if coreBlk.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of sibling")
	}

	// ...finally accept ProBlk1
	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not reject ProBlk1")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("ProBlk2 should stay rejected")
	}
	if coreBlk.Status() != choices.Accepted {
		t.Fatal("coreBlk1 should be accepted")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("coreBlk3 should still be processing")
	}
	if proBlk1.Status() != choices.Accepted {
		t.Fatal("proBlk1 should be accepted")
	}
}

func TestReject_ChildFirst_MixedCoreBlocksConflict(t *testing.T) {
	// proBlk1 and proBlk2 wrap the same coreBlocks coreBlk and are siblings; proBlk2 has a child proBlk3
	// proBlk3 and proBlk2 are rejected in this order, then proBlk1 is accepted
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	pChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return pChainHeight, nil }
	nodeID, err := ids.ToShortID(hashing.PubkeyBytesToAddress(pTestCert.Leaf.Raw))
	if err != nil {
		t.Fatal("Could not evalute nodeID")
	}
	valVM.GetValidatorsF = func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
		res := make(map[ids.ShortID]uint64)
		res[nodeID] = uint64(10)
		return res, nil
	}

	// generate proBlk1 and proBlk2,proBlk3 chain with different coreBlocks
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(1111),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{1},
		ParentV: coreGenBlk,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	pChainHeight = uint64(4000) // move ahead P-Chain height so that proBlk1 != proBlk2
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("test requires proBlk1 different from proBlk2")
	}

	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("could not set preference")
	}

	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(3333),
			StatusV: choices.Processing,
		},
		BytesV:  []byte{3},
		ParentV: coreBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk3, nil }
	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.Bytes()):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		case bytes.Equal(b, coreBlk3.Bytes()):
			return coreBlk3, nil
		default:
			t.Fatal("Parsing unknown core block")
			return nil, nil
		}
	}

	// ..Reject ProBlk3 ...
	if err := proBlk3.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk3")
	}
	if proBlk2.Status() != choices.Processing {
		t.Fatal("ProBlk2 should be processing")
	}
	if coreBlk.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of siblings' descendants")
	}

	//  ... and ProBlk2,...
	if err := proBlk2.Reject(); err != nil {
		t.Fatal("Could not reject ProBlk2")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("proBlk2 should be Rejected")
	}
	if coreBlk.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("proBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("Rejection of ProBlock does not cause rejection of its coreBlk")
	}
	if proBlk1.Status() != choices.Processing {
		t.Fatal("proBlk1 should be unaffected by rejection of sibling")
	}

	// ...finally accept ProBlk1
	if err := proBlk1.Accept(); err != nil {
		t.Fatal("Could not reject ProBlk1")
	}
	if proBlk2.Status() != choices.Rejected {
		t.Fatal("ProBlk2 should stay rejected")
	}
	if coreBlk.Status() != choices.Accepted {
		t.Fatal("coreBlk1 should be accepted")
	}
	if proBlk3.Status() != choices.Rejected {
		t.Fatal("ProBlk3 should stay rejected")
	}
	if coreBlk3.Status() != choices.Processing {
		t.Fatal("coreBlk3 should still be processing")
	}
	if proBlk1.Status() != choices.Accepted {
		t.Fatal("proBlk1 should be accepted")
	}
}
