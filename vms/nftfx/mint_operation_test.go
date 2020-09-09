package nftfx

import (
	"testing"

	"github.com/ava-labs/avalanche-go/vms/components/verify"
	"github.com/ava-labs/avalanche-go/vms/secp256k1fx"
)

func TestMintOperationVerifyNil(t *testing.T) {
	op := (*MintOperation)(nil)
	if err := op.Verify(); err == nil {
		t.Fatalf("nil operation should have failed verification")
	}
}

func TestMintOperationVerifyTooLargePayload(t *testing.T) {
	op := MintOperation{
		Payload: make([]byte, MaxPayloadSize+1),
	}
	if err := op.Verify(); err == nil {
		t.Fatalf("operation should have failed verification")
	}
}

func TestMintOperationVerifyInvalidOutput(t *testing.T) {
	op := MintOperation{
		Outputs: []*secp256k1fx.OutputOwners{{
			Threshold: 1,
		}},
	}
	if err := op.Verify(); err == nil {
		t.Fatalf("operation should have failed verification")
	}
}

func TestMintOperationOuts(t *testing.T) {
	op := MintOperation{
		Outputs: []*secp256k1fx.OutputOwners{{}},
	}
	if outs := op.Outs(); len(outs) != 1 {
		t.Fatalf("Wrong number of outputs returned")
	}
}

func TestMintOperationState(t *testing.T) {
	intf := interface{}(&MintOperation{})
	if _, ok := intf.(verify.State); ok {
		t.Fatalf("shouldn't be marked as state")
	}
}
