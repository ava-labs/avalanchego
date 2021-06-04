package proposervm

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/missing"
)

const BlkSubmissionTolerance = 10 * time.Second // Todo: move to consensus?

var (
	ErrInnerBlockNotOracle = errors.New("snowmanBlock wrapped in proposerBlock does not implement snowman.OracleBlock")
	ErrProBlkNotFound      = errors.New("snowmanBlock not found")
	ErrProBlkBadTimestamp  = errors.New("snowman block timestamp outside tolerance window")
)

type ProposerBlockHeader struct {
	PrntID    ids.ID `serialize:"true" json:"parentID"`
	Timestamp int64  `serialize:"true"`
}

func NewProHeader(prntID ids.ID, unixTime int64) ProposerBlockHeader {
	return ProposerBlockHeader{
		PrntID:    prntID,
		Timestamp: unixTime,
	}
}

type ProposerBlock struct {
	header ProposerBlockHeader
	snowman.Block
	id    ids.ID
	bytes []byte
	vm    *VM
}

func NewProBlock(vm *VM, hdr ProposerBlockHeader, sb snowman.Block) ProposerBlock {
	res := ProposerBlock{
		header: hdr,
		Block:  sb,
		bytes:  nil,
		vm:     vm,
	}
	res.bytes = res.Bytes()
	res.id = hashing.ComputeHash256Array(res.bytes)
	return res
}

//////// choices.Decidable interface implementation
func (pb *ProposerBlock) ID() ids.ID {
	return pb.id
}

func (pb *ProposerBlock) Accept() error {
	return pb.Block.Accept()
}

func (pb *ProposerBlock) Reject() error {
	return pb.Block.Reject()
}

func (pb *ProposerBlock) Status() choices.Status {
	return pb.Block.Status()
}

//////// snowman.Block interface implementation
func (pb *ProposerBlock) Parent() snowman.Block {
	res, ok := pb.vm.knownProBlocks[pb.header.PrntID]
	if !ok {
		return &missing.Block{BlkID: pb.header.PrntID}
	}
	return res
}

func (pb *ProposerBlock) Verify() error {
	if err := pb.Block.Verify(); err != nil {
		return err
	}

	prntBlk, ok := pb.vm.knownProBlocks[pb.header.PrntID]
	if !ok {
		return ErrProBlkNotFound
	}

	if pb.header.Timestamp < prntBlk.header.Timestamp {
		return ErrProBlkBadTimestamp
	}

	if time.Unix(pb.header.Timestamp, 0).After(time.Now().Add(BlkSubmissionTolerance)) {
		return ErrProBlkBadTimestamp
	}

	return nil
}

func (pb *ProposerBlock) Bytes() []byte {
	if pb.bytes != nil {
		return pb.bytes
	}

	hdrBytes, err := cdc.Marshal(codecVersion, &pb.header)
	if err != nil {
		pb.bytes = make([]byte, 0)
		return pb.bytes
	}

	wrpdBytes := pb.Block.Bytes()
	hdrBytes = append(hdrBytes, wrpdBytes...)
	return hdrBytes
}

func (pb *ProposerBlock) Height() uint64 {
	return pb.Block.Height()
}

//////// snowman.OracleBlock interface implementation
func (pb *ProposerBlock) Options() ([2]snowman.Block, error) {
	if oracleBlk, ok := pb.Block.(snowman.OracleBlock); ok {
		return oracleBlk.Options()
	}

	return [2]snowman.Block{}, ErrInnerBlockNotOracle
}
