package propertyfx

import (
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// OwnedOutput ...
type OwnedOutput struct {
	secp256k1fx.OutputOwners `serialize:"true"`
}
