package nftfx

import (
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

// MintOutput ...
type MintOutput struct {
	GroupID                  uint32 `serialize:"true"`
	secp256k1fx.OutputOwners `serialize:"true"`
}
