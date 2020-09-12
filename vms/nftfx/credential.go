package nftfx

import (
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Credential ...
type Credential struct {
	secp256k1fx.Credential `serialize:"true"`
}
