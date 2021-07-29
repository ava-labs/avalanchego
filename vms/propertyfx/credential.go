package propertyfx

import (
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type Credential struct {
	secp256k1fx.Credential `serialize:"true"`
}
