package propertyfx

import (
	"github.com/ava-labs/avalanche-go/vms/secp256k1fx"
)

// OwnedOutput ...
type OwnedOutput struct {
	secp256k1fx.OutputOwners `serialize:"true"`
}
