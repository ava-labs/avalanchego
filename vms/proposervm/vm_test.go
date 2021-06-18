package proposervm

import (
	"bytes"
	"crypto/tls"
	"errors"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/staking"
)

var (
	pTestCert          *tls.Certificate = nil // package variable to init it only once
	errGetValidatorSet                  = errors.New("unexpectedly called GetValidatorSet")
	errCurrentHeight                    = errors.New("unexpectedly called GetCurrentHeigh")
)

type TestValidatorVM struct {
	T *testing.T
	CantGetValidatorSet,
	CantGetCurrentHeight bool

	GetValidatorsF    func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error)
	GetCurrentHeightF func() (uint64, error)
}

func (tVM *TestValidatorVM) GetValidatorSet(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
	if tVM.GetValidatorsF != nil {
		return tVM.GetValidatorsF(height, subnetID)
	}
	if tVM.CantGetValidatorSet && tVM.T != nil {
		tVM.T.Fatal(errGetValidatorSet)
	}
	return nil, errGetValidatorSet
}

func (tVM *TestValidatorVM) GetCurrentHeight() (uint64, error) {
	if tVM.GetCurrentHeightF != nil {
		return tVM.GetCurrentHeightF()
	}
	if tVM.CantGetCurrentHeight && tVM.T != nil {
		tVM.T.Fatal(errCurrentHeight)
	}
	return 0, errCurrentHeight
}

// VM.Initialize tests section
func TestInitializeRecordsGenesis(t *testing.T) {
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks

	proGenID := proVM.state.proGenID
	if _, err := proVM.state.getProBlock(proGenID); err != nil {
		t.Fatal("Could not retrieve proposer genesis block")
	}
	proGenBlk, err := proVM.state.getProGenesisBlk()
	if err != nil {
		t.Fatal("Could not retrieve proposer genesis block")
	}
	if proGenBlk.ID() != proGenID {
		t.Fatal("Inconsistent genesis block information")
	}
	if proGenBlk.coreBlk.ID() != coreGenBlk.ID() {
		t.Fatal("proposer genesis block does not wrap core genesis block")
	}

	// clear cache and check persistence
	proVM.state.wipeCache()

	coreVM.CantParseBlock = true // make sure coreGenBlk is parsable
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, coreGenBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return coreGenBlk, nil
	}

	proGenBlk, err = proVM.state.getProGenesisBlk()
	if err != nil {
		t.Fatal("Could not retrieve proposer genesis block after wiping out cache")
	}
	if proGenBlk.ID() != proGenID {
		t.Fatal("Inconsistent genesis block information after wiping out cache")
	}
}

func initTestProposerVM(t *testing.T, proBlkStartTime time.Time) (*block.TestVM, *TestValidatorVM, *VM, *snowman.TestBlock) {
	// setup
	coreGenBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(0000),
			StatusV: choices.Unknown,
		},
		BytesV:  []byte{0},
		HeightV: 0,
	}

	coreVM := &block.TestVM{}
	coreVM.CantInitialize = true
	coreVM.InitializeF = func(*snow.Context, manager.Manager, []byte, []byte, []byte, chan<- common.Message, []*common.Fx) error {
		return nil
	}
	coreVM.LastAcceptedF = func() (ids.ID, error) { return coreGenBlk.ID(), nil }
	coreVM.GetBlockF = func(ids.ID) (snowman.Block, error) { return coreGenBlk, nil }

	tc := &testClock{
		setTime: time.Now(),
	}
	proVM := NewProVM(coreVM, proBlkStartTime)
	proVM.clock = tc

	if pTestCert == nil {
		var err error
		pTestCert, err = staking.NewTLSCert()
		if err != nil {
			t.Fatal("Could not generate dummy StakerCert")
		}
	}

	valVM := &TestValidatorVM{
		T: t,
	}

	ctx := &snow.Context{
		StakingCert: *pTestCert,
		ValidatorVM: valVM,
	}
	dummyDBManager := manager.NewDefaultMemDBManager()
	if err := proVM.Initialize(ctx, dummyDBManager, coreGenBlk.Bytes(), nil, nil, nil, nil); err != nil {
		t.Fatal("failed to initialize proposerVM")
	}
	return coreVM, valVM, proVM, coreGenBlk
}

// VM.BuildBlock tests section
func TestBuildBlockRecordsAndVerifiesBuiltBlock(t *testing.T) {
	// setup
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return 2000, nil }

	coreVM.CantBuildBlock = true
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(2021),
		},
		ParentV: coreGenBlk,
		VerifyV: nil,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	// test
	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}
	if err := builtBlk.Verify(); err != nil {
		t.Fatal("built block should be verified")
	}

	// test
	coreVM.CantGetBlock = false // forbid calls to coreVM to show caching
	storedBlk, err := proVM.GetBlock(builtBlk.ID())
	if err != nil {
		t.Fatal("proposerVM has not cached built block")
	}
	if storedBlk != builtBlk {
		t.Fatal("proposerVM retrieved wrong block")
	}
}

func TestBuildBlockIsIdempotent(t *testing.T) {
	// given the same core block, BuildBlock returns the same proposer block
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return 2000, nil }

	coreVM.CantBuildBlock = true
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(111),
		},
		ParentV: coreGenBlk,
		VerifyV: nil,
	}

	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }

	// test
	builtBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}
	builtBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}

	if !bytes.Equal(builtBlk1.Bytes(), builtBlk2.Bytes()) {
		t.Fatal("proposer blocks wrapping the same core block are different")
	}
}

func TestFirstProposerBlockIsBuiltOnTopOfGenesis(t *testing.T) {
	// setup
	coreVM, valVM, proVM, genesisBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return 2000, nil }

	newBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(2021),
		},
		ParentV: genesisBlk,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return newBlk, nil }

	// test
	snowBlock, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build block")
	}

	// checks
	proBlock, ok := snowBlock.(*ProposerBlock)
	if !ok {
		t.Fatal("proposerVM.BuildBlock() does not return a proposervm.Block")
	}

	if proBlock.coreBlk != newBlk {
		t.Fatal("different block was expected to be built")
	}

	if proBlock.Parent().ID() == genesisBlk.ID() {
		t.Fatal("first block not built on genesis")
	}
}

// both core blocks and pro blocks must be built on preferred
func TestProposerBlocksAreBuiltOnPreferredProBlock(t *testing.T) {
	coreVM, valVM, proVM, genCoreBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// add to proBlks...
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(111),
		},
		BytesV:  []byte{1},
		ParentV: genCoreBlk,
		HeightV: genCoreBlk.Height() + 1,
	}
	proGenID, _ := proVM.LastAccepted()
	proHdr1 := NewProHeader(proGenID, coreBlk1.Timestamp().Unix(), currentPChainHeight, *pTestCert.Leaf)
	proBlk1, err := NewProBlock(proVM, proHdr1, coreBlk1, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign proposert block")
	}
	if err := proVM.state.storeProBlk(&proBlk1); err != nil {
		t.Fatal("Could not store proposerBlock")
	}

	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(222),
		},
		BytesV:  []byte{2},
		ParentV: genCoreBlk,
		HeightV: genCoreBlk.Height() + 1,
	}
	proHdr2 := NewProHeader(proGenID, coreBlk2.Timestamp().Unix(), currentPChainHeight, *pTestCert.Leaf)
	proBlk2, err := NewProBlock(proVM, proHdr2, coreBlk2, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign proposert block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("proBlk1 and proBlk2 should be different for this test")
	}
	if err := proVM.state.storeProBlk(&proBlk2); err != nil {
		t.Fatal("Could not store proposerBlock")
	}

	// ...and set one as preferred
	var prefCoreBlk *snowman.TestBlock
	coreVM.CantSetPreference = true
	coreVM.SetPreferenceF = func(prefID ids.ID) error {
		switch prefID {
		case coreBlk1.ID():
			prefCoreBlk = coreBlk1
			return nil
		case coreBlk2.ID():
			prefCoreBlk = coreBlk2
			return nil
		default:
			t.Fatal("Unknown core Blocks set as preferred")
			return nil
		}
	}
	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("Could not set preference")
	}

	// build block...
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) {
		coreBuiltBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV: ids.Empty.Prefix(333),
			},
			BytesV:  []byte{3},
			ParentV: prefCoreBlk,
			HeightV: prefCoreBlk.Height() + 1,
		}
		return coreBuiltBlk, nil
	}
	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("could not build block")
	}

	// ...show that parent is the preferred one
	if builtBlk.Parent().ID() != proBlk2.ID() {
		t.Fatal("proposer block not built on preferred parent")
	}
}

func TestCoreBlocksMustBeBuiltOnPreferredCoreBlock(t *testing.T) {
	coreVM, valVM, proVM, genCoreBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// add to proBlks...
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(111),
		},
		BytesV:  []byte{1},
		ParentV: genCoreBlk,
		HeightV: genCoreBlk.Height() + 1,
	}
	proGenID, _ := proVM.LastAccepted()
	proHdr1 := NewProHeader(proGenID, coreBlk1.Timestamp().Unix(), currentPChainHeight, *pTestCert.Leaf)
	proBlk1, err := NewProBlock(proVM, proHdr1, coreBlk1, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign proposert block")
	}
	if err := proVM.state.storeProBlk(&proBlk1); err != nil {
		t.Fatal("Could not store proposerBlock")
	}

	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(222),
		},
		BytesV:  []byte{2},
		ParentV: genCoreBlk,
		HeightV: genCoreBlk.Height() + 1,
	}
	proHdr2 := NewProHeader(proGenID, coreBlk2.Timestamp().Unix(), currentPChainHeight, *pTestCert.Leaf)
	proBlk2, err := NewProBlock(proVM, proHdr2, coreBlk2, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign proposert block")
	}
	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("proBlk1 and proBlk2 should be different for this test")
	}
	if err := proVM.state.storeProBlk(&proBlk2); err != nil {
		t.Fatal("Could not store proposerBlock")
	}

	// ...and set one as preferred
	var wronglyPreferredCoreBlk *snowman.TestBlock
	coreVM.CantSetPreference = true
	coreVM.SetPreferenceF = func(prefID ids.ID) error {
		switch prefID {
		case coreBlk1.ID():
			wronglyPreferredCoreBlk = coreBlk2
			return nil
		case coreBlk2.ID():
			wronglyPreferredCoreBlk = coreBlk1
			return nil
		default:
			t.Fatal("Unknown core Blocks set as preferred")
			return nil
		}
	}
	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("Could not set preference")
	}

	// build block...
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) {
		coreBuiltBlk := &snowman.TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV: ids.Empty.Prefix(333),
			},
			BytesV:  []byte{3},
			ParentV: wronglyPreferredCoreBlk,
			HeightV: wronglyPreferredCoreBlk.Height() + 1,
		}
		return coreBuiltBlk, nil
	}
	if _, err := proVM.BuildBlock(); err != ErrProBlkWrongParent {
		t.Fatal("coreVM does not build on preferred coreBlock. It should err")
	}
}

// VM.ParseBlock tests section
func TestParseBlockRecordsButDoesNotVerifyParsedBlock(t *testing.T) {
	coreVM, valVM, proVM, genCoreBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	coreBlkDoesNotVerify := errors.New("coreBlk should not verify in this test")
	coreBlk := &snowman.TestBlock{
		BytesV:     []byte{1},
		VerifyV:    coreBlkDoesNotVerify,
		ParentV:    genCoreBlk,
		TimestampV: time.Now().AddDate(0, 0, -1),
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, coreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return coreBlk, nil
	}

	proGenBlkID, _ := proVM.LastAccepted()
	proHdr := NewProHeader(proGenBlkID, coreBlk.Timestamp().Unix(), currentPChainHeight, *pTestCert.Leaf)
	proBlk, err := NewProBlock(proVM, proHdr, coreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign proposert block")
	}

	// test
	parsedBlk, err := proVM.ParseBlock(proBlk.Bytes())
	if err != nil {
		t.Fatal("proposerVM could not parse block")
	}
	if err := parsedBlk.Verify(); err == nil {
		t.Fatal("parsed block should not necessarily verify upon parse")
	}

	// test
	coreVM.CantGetBlock = false // forbid calls to coreVM to show caching
	storedBlk, err := proVM.GetBlock(parsedBlk.ID())
	if err != nil {
		t.Fatal("proposerVM has not cached parsed block")
	}
	if storedBlk != parsedBlk {
		t.Fatal("proposerVM retrieved wrong block")
	}
}

func TestTwoProBlocksWrappingSameCoreBlockCanBeParsed(t *testing.T) {
	coreVM, valVM, proVM, genCoreBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// create two Proposer blocks at the same height
	coreBlk := &snowman.TestBlock{
		BytesV:  []byte{1},
		ParentV: genCoreBlk,
		HeightV: genCoreBlk.Height() + 1,
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, coreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return coreBlk, nil
	}

	proGenBlkID, _ := proVM.LastAccepted()
	proHdr1 := NewProHeader(proGenBlkID, coreBlk.Timestamp().Unix(), currentPChainHeight, *pTestCert.Leaf)
	proBlk1, err := NewProBlock(proVM, proHdr1, coreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign proposert block")
	}

	proHdr2 := NewProHeader(proGenBlkID, coreBlk.Timestamp().Add(time.Second).Unix(), currentPChainHeight, *pTestCert.Leaf)
	proBlk2, err := NewProBlock(proVM, proHdr2, coreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign proposert block")
	}

	if proBlk1.ID() == proBlk2.ID() {
		t.Fatal("Test requires proBlk1 and proBlk2 to be different")
	}

	// Show that both can be parsed and retrieved
	parsedBlk1, err := proVM.ParseBlock(proBlk1.Bytes())
	if err != nil {
		t.Fatal("proposerVM could not parse parsedBlk1")
	}
	parsedBlk2, err := proVM.ParseBlock(proBlk2.Bytes())
	if err != nil {
		t.Fatal("proposerVM could not parse parsedBlk2")
	}

	if parsedBlk1.ID() != proBlk1.ID() {
		t.Fatal("error in parsing block")
	}
	if parsedBlk2.ID() != proBlk2.ID() {
		t.Fatal("error in parsing block")
	}

	rtrvdProBlk1, err := proVM.GetBlock(proBlk1.ID())
	if err != nil {
		t.Fatal("Could not retrieve proBlk1")
	}
	if rtrvdProBlk1.ID() != proBlk1.ID() {
		t.Fatal("blocks do not match following cache whiping")
	}

	rtrvdProBlk2, err := proVM.GetBlock(proBlk2.ID())
	if err != nil {
		t.Fatal("Could not retrieve proBlk1")
	}
	if rtrvdProBlk2.ID() != proBlk2.ID() {
		t.Fatal("blocks do not match following cache whiping")
	}
}

// VM.BuildBlock and VM.ParseBlock interoperability tests section
func TestTwoProBlocksWithSameParentCanBothVerify(t *testing.T) {
	coreVM, valVM, proVM, genCoreBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	currentPChainHeight := uint64(2000)
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return currentPChainHeight, nil }

	// one block is built from this proVM
	localCoreBlk := &snowman.TestBlock{
		BytesV:  []byte{111},
		ParentV: genCoreBlk,
		HeightV: genCoreBlk.Height() + 1,
	}
	coreVM.CantBuildBlock = true
	coreVM.BuildBlockF = func() (snowman.Block, error) {
		return localCoreBlk, nil
	}

	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build block")
	}
	if err = builtBlk.Verify(); err != nil {
		t.Fatal("Built block does not verify")
	}

	// another block with same parent comes from network and is parsed
	netCoreBlk := &snowman.TestBlock{
		BytesV:  []byte{222},
		ParentV: genCoreBlk,
		HeightV: genCoreBlk.Height() + 1,
	}
	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, netCoreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return netCoreBlk, nil
	}

	proGenBlkID, _ := proVM.LastAccepted()
	netHdr := NewProHeader(proGenBlkID, netCoreBlk.Timestamp().Unix(), currentPChainHeight, *pTestCert.Leaf)
	netProBlk, err := NewProBlock(proVM, netHdr, netCoreBlk, choices.Processing, nil, true)
	if err != nil {
		t.Fatal("could not sign proposert block")
	}

	// prove that also block from network verifies
	if err = netProBlk.Verify(); err != nil {
		t.Fatal("block from network does not verify")
	}
}

// VM persistency test section
func TestProposerVMCacheCanBeRebuiltFromDB(t *testing.T) {
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, time.Unix(0, 0)) // enable ProBlks
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return 2000, nil }

	// build two blocks on top of genesis
	coreBlk1 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.GenerateTestID(),
		},
		ParentV: coreGenBlk,
		BytesV:  []byte{1},
		VerifyV: nil,
	}
	coreBlk2 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.GenerateTestID(),
		},
		ParentV: coreBlk1,
		BytesV:  []byte{2},
		VerifyV: nil,
	}
	coreBuildCalls := 0
	coreVM.BuildBlockF = func() (snowman.Block, error) {
		switch coreBuildCalls {
		case 0:
			coreBuildCalls++
			return coreBlk1, nil
		case 1:
			coreBuildCalls++
			return coreBlk2, nil
		default:
			t.Fatal("BuildBlock of coreVM called too many times")
		}
		return nil, nil
	}
	proBlk1, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build block")
	}

	// update preference to build next block
	if err := proVM.SetPreference(proBlk1.ID()); err != nil {
		t.Fatal("Could not set preference")
	}
	proBlk2, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build block")
	}

	// while inner cache, as it would happen upon node shutdown
	proVM.state.wipeCache()

	// build a new block to show ops can resume smoothly
	coreBlk3 := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.GenerateTestID(),
		},
		ParentV: coreBlk2,
		BytesV:  []byte{3},
		VerifyV: nil,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) {
		return coreBlk3, nil
	}
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		switch {
		case bytes.Equal(b, coreGenBlk.BytesV):
			return coreGenBlk, nil
		case bytes.Equal(b, coreBlk1.BytesV):
			return coreBlk1, nil
		case bytes.Equal(b, coreBlk2.BytesV):
			return coreBlk2, nil
		default:
			t.Fatal("ParseBlock of coreVM called with unknown block")
		}
		return nil, nil
	}

	// update preference to build next block
	if err := proVM.SetPreference(proBlk2.ID()); err != nil {
		t.Fatal("Could not set preference")
	}
	proBlk3, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build block")
	}

	if proBlk3.Parent().ID() != proBlk2.ID() {
		t.Fatal("Error in building Block")
	}

	// while inner cache, as it would happen upon node shutdown
	proVM.state.wipeCache()

	// check that getBlock still works on older blocks
	rtrvdProBlk2, err := proVM.GetBlock(proBlk2.ID())
	if err != nil {
		t.Fatal("Could not get block after whiping off proposerVM cache")
	}
	if rtrvdProBlk2.ID() != proBlk2.ID() {
		t.Fatal("blocks do not match following cache whiping")
	}
	if err = rtrvdProBlk2.Verify(); err != nil {
		t.Fatal("block retrieved after cache whiping does not verify")
	}

	rtrvdProBlk1, err := proVM.GetBlock(proBlk1.ID())
	if err != nil {
		t.Fatal("Could not get block after whiping off proposerVM cache")
	}
	if rtrvdProBlk1.ID() != proBlk1.ID() {
		t.Fatal("blocks do not match following cache whiping")
	}
	if err = rtrvdProBlk1.Verify(); err != nil {
		t.Fatal("block retrieved after cache whiping does not verify")
	}
}

// VM backward compatibility tests section
func TestPreSnowmanInitialize(t *testing.T) {
	_, _, proVM, genesisBlk := initTestProposerVM(t, NoProposerBlocks) // disable ProBlks

	// checks
	blkID, err := proVM.LastAccepted()
	if err != nil {
		t.Fatal("failed to retrieve last accepted block")
	}

	rtvdBlk, err := proVM.GetBlock(blkID)
	if err != nil {
		t.Fatal("Block should be returned without calling core vm")
	}

	if _, ok := rtvdBlk.(*ProposerBlock); ok {
		t.Fatal("retrieved block should not be a proposer block")
	}

	if !bytes.Equal(rtvdBlk.Bytes(), genesisBlk.Bytes()) {
		t.Fatal("Stored block is not genesis")
	}
}

func TestPreSnowmanBuildBlock(t *testing.T) {
	// setup
	coreVM, _, proVM, coreGenBlk := initTestProposerVM(t, NoProposerBlocks) // disable ProBlks
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

	// test
	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("proposerVM could not build block")
	}
	if builtBlk.ID() != coreBlk.ID() {
		t.Fatal("unexpected built block")
	}

	// test
	coreVM.CantGetBlock = true
	coreVM.GetBlockF = func(id ids.ID) (snowman.Block, error) { return coreBlk, nil }
	storedBlk, err := proVM.GetBlock(builtBlk.ID())
	if err != nil {
		t.Fatal("proposerVM has not cached built block")
	}
	if storedBlk != builtBlk {
		t.Fatal("proposerVM retrieved wrong block")
	}
}

func TestPreSnowmanParseBlock(t *testing.T) {
	// setup
	coreVM, _, proVM, _ := initTestProposerVM(t, NoProposerBlocks) // disable ProBlks

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(2021),
		},
		BytesV: []byte{0},
	}

	coreVM.CantParseBlock = true
	coreVM.ParseBlockF = func(b []byte) (snowman.Block, error) {
		if !bytes.Equal(b, coreBlk.Bytes()) {
			t.Fatalf("Wrong bytes")
		}
		return coreBlk, nil
	}

	parsedBlk, err := proVM.ParseBlock(coreBlk.Bytes())
	if err != nil {
		t.Fatal("Could not parse naked core block")
	}
	if parsedBlk.ID() != coreBlk.ID() {
		t.Fatal("Parsed block does not match expected block")
	}

	coreVM.CantGetBlock = true
	coreVM.GetBlockF = func(id ids.ID) (snowman.Block, error) {
		if id != coreBlk.ID() {
			t.Fatalf("Unknown core block")
		}
		return coreBlk, nil
	}
	storedBlk, err := proVM.GetBlock(parsedBlk.ID())
	if err != nil {
		t.Fatal("proposerVM has not cached parsed block")
	}
	if storedBlk != parsedBlk {
		t.Fatal("proposerVM retrieved wrong block")
	}
}

func TestPreSnowmanSetPreference(t *testing.T) {
	coreVM, valVM, proVM, coreGenBlk := initTestProposerVM(t, NoProposerBlocks) // disable ProBlks
	valVM.CantGetCurrentHeight = true
	valVM.GetCurrentHeightF = func() (uint64, error) { return 2000, nil }

	coreVM.CantBuildBlock = true
	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV: ids.Empty.Prefix(10),
		},
		ParentV: coreGenBlk,
		VerifyV: nil,
	}
	coreVM.BuildBlockF = func() (snowman.Block, error) { return coreBlk, nil }
	builtBlk, err := proVM.BuildBlock()
	if err != nil {
		t.Fatal("Could not build proposer block")
	}

	// test
	if err = proVM.SetPreference(builtBlk.ID()); err != nil {
		t.Fatal("Could not set preference on proposer Block")
	}
}

func TestPreSnowmanBlockAccept(t *testing.T) {
	coreVM, _, proVM, _ := initTestProposerVM(t, NoProposerBlocks) // disable ProBlks

	coreBlk := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(10),
			StatusV: choices.Accepted,
		},
	}

	coreVM.CantLastAccepted = true
	coreVM.LastAcceptedF = func() (ids.ID, error) {
		return coreBlk.ID(), nil
	}

	// test
	laID, err := proVM.LastAccepted()
	if err != nil {
		t.Fatal("Could not retrieve last accepted proposer Block ID")
	}
	if laID != coreBlk.ID() {
		t.Fatal("Unexpected last accepted proposer block ID")
	}
}
