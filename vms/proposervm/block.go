package proposervm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

type ProposerBlock struct {
	wrappedBlock snowman.Block
}

func NewProBlock(sb snowman.Block) ProposerBlock {
	return ProposerBlock{
		wrappedBlock: sb,
	}
}

func (pb ProposerBlock) GetWrappedBlock() snowman.Block {
	return pb.wrappedBlock
}

//////// choices.Decidable interface implementation
func (pb ProposerBlock) ID() ids.ID {
	return pb.wrappedBlock.ID()
}

func (pb ProposerBlock) Accept() error {
	return pb.wrappedBlock.Accept()
}

func (pb ProposerBlock) Reject() error {
	return pb.wrappedBlock.Reject()
}

func (pb ProposerBlock) Status() choices.Status {
	return pb.wrappedBlock.Status()
}

//////// snowman.Block interface implementation
func (pb ProposerBlock) Parent() snowman.Block {
	return pb.wrappedBlock.Parent()
}

func (pb ProposerBlock) Verify() error {
	return pb.wrappedBlock.Verify() // here new block fields will be handled
}

func (pb ProposerBlock) Bytes() []byte {
	return pb.wrappedBlock.Bytes()
}

func (pb ProposerBlock) Height() uint64 {
	return pb.wrappedBlock.Height()
}
