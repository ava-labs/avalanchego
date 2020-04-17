package nftfx

import (
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

// Credential ...
type Credential struct {
	secp256k1fx.Credential `serialize:"true"`
}
