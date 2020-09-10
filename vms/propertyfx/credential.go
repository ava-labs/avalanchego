package propertyfx

import (
	"github.com/ava-labs/avalanche-go/vms/secp256k1fx"
)

// Credential ...
type Credential struct {
	secp256k1fx.Credential `serialize:"true"`
}
