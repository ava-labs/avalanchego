package proposervm

import (
	"crypto"
	cryptorand "crypto/rand"
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/missing"
)

const (
	BlkSubmissionTolerance = 10 * time.Second
	BlkSubmissionWinLength = 2 * time.Second
)

var (
	ErrInnerBlockNotOracle = errors.New("core snowman block does not implement snowman.OracleBlock")
	ErrProBlkNotFound      = errors.New("proposer block not found")
	ErrProBlkBadTimestamp  = errors.New("proposer block timestamp outside tolerance window")
	ErrProBlkWrongHeight   = errors.New("proposer block has wrong height")
)

type ProposerBlockHeader struct {
	PrntID       ids.ID `serialize:"true" json:"parentID"`
	Timestamp    int64  `serialize:"true"`
	PChainHeight uint64 `serialize:"true"`
	Signature    []byte `serialize:"true"`
}

func NewProHeader(prntID ids.ID, unixTime int64, height uint64) ProposerBlockHeader {
	return ProposerBlockHeader{
		PrntID:       prntID,
		Timestamp:    unixTime,
		PChainHeight: height,
	}
}

type marshallingProposerBLock struct {
	Header    ProposerBlockHeader `serialize:"true"`
	WrpdBytes []byte              `serialize:"true"`
}

type ProposerBlock struct {
	header  ProposerBlockHeader
	coreBlk snowman.Block
	id      ids.ID
	bytes   []byte
	vm      *VM
}

func NewProBlock(vm *VM, hdr ProposerBlockHeader, sb snowman.Block, bytes []byte, signBlk bool) (ProposerBlock, error) {
	res := ProposerBlock{
		header:  hdr,
		coreBlk: sb,
		bytes:   bytes,
		vm:      vm,
	}

	if signBlk { // should be done before calling Bytes()
		if err := res.sign(); err != nil {
			return ProposerBlock{}, err
		}
	}

	if bytes == nil {
		res.bytes = res.Bytes()
	}

	res.id = hashing.ComputeHash256Array(res.bytes)
	return res, nil
}

func (pb *ProposerBlock) sign() error {
	pb.header.Signature = nil
	msgHash := hashing.ComputeHash256Array(pb.getBytes())
	sig, err := pb.vm.stakingKey.Sign(cryptorand.Reader, msgHash[:], crypto.SHA256)
	if err != nil {
		return err
	}
	pb.header.Signature = sig
	return nil
}

// choices.Decidable interface implementation
func (pb *ProposerBlock) ID() ids.ID {
	return pb.id
}

func (pb *ProposerBlock) Accept() error {
	err := pb.coreBlk.Accept()
	if err == nil {
		// pb parent block should not be needed anymore.
		pb.vm.state.wipeFromCacheProBlk(pb.header.PrntID)
	}
	return err
}

func (pb *ProposerBlock) Reject() error {
	err := pb.coreBlk.Reject()
	if err == nil {
		pb.vm.state.wipeFromCacheProBlk(pb.id)
	}
	return err
}

func (pb *ProposerBlock) Status() choices.Status {
	return pb.coreBlk.Status()
}

// snowman.Block interface implementation
func (pb *ProposerBlock) Parent() snowman.Block {
	if res, err := pb.vm.state.getProBlock(pb.header.PrntID); err == nil {
		return res
	}

	return &missing.Block{BlkID: pb.header.PrntID}
}

func (pb *ProposerBlock) Verify() error {
	if err := pb.coreBlk.Verify(); err != nil {
		return err
	}

	prntBlk, err := pb.vm.state.getProBlock(pb.header.PrntID)
	if err != nil {
		return ErrProBlkNotFound
	}

	if pb.header.PChainHeight < prntBlk.header.PChainHeight {
		return ErrProBlkWrongHeight
	}

	if pb.header.PChainHeight > pb.vm.pChainHeight() {
		return ErrProBlkWrongHeight
	}

	if pb.header.Timestamp < prntBlk.header.Timestamp {
		return ErrProBlkBadTimestamp
	}

	nodeID := ids.ID{} // TODO: retrieve nodeID from signature
	blkWinDelay := pb.vm.BlkSubmissionDelay(pb.header.PChainHeight, nodeID)
	blkWinStart := time.Unix(prntBlk.header.Timestamp, 0).Add(blkWinDelay)
	if time.Unix(pb.header.Timestamp, 0).Before(blkWinStart) {
		return ErrProBlkBadTimestamp
	}

	if time.Unix(pb.header.Timestamp, 0).After(pb.vm.now().Add(BlkSubmissionTolerance)) {
		return ErrProBlkBadTimestamp
	}

	return nil
}

func (pb *ProposerBlock) getBytes() []byte {
	var mPb marshallingProposerBLock
	mPb.Header = pb.header
	mPb.WrpdBytes = pb.coreBlk.Bytes()

	var err error
	var res []byte
	res, err = cdc.Marshal(codecVersion, &mPb)
	if err != nil {
		res = make([]byte, 0)
	}
	return res
}

func (pb *ProposerBlock) Bytes() []byte {
	if pb.bytes == nil {
		pb.bytes = pb.getBytes()
	}
	return pb.bytes
}

func (pb *ProposerBlock) Height() uint64 {
	return pb.coreBlk.Height()
}

// snowman.OracleBlock interface implementation
func (pb *ProposerBlock) Options() ([2]snowman.Block, error) {
	if oracleBlk, ok := pb.coreBlk.(snowman.OracleBlock); ok {
		return oracleBlk.Options()
	}

	return [2]snowman.Block{}, ErrInnerBlockNotOracle
}
