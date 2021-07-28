package nftfx

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type MintOutput struct {
	snow.ContextInitializable `serialize:"false" json:"-"`
	GroupID                   uint32 `serialize:"true" json:"groupID"`
	secp256k1fx.OutputOwners  `serialize:"true"`
}

func (out *MintOutput) InitCtx(ctx *snow.Context) {
	out.OutputOwners.InitCtx(ctx)
}
