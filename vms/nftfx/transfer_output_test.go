package nftfx

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func TestTransferOutputVerifyNil(t *testing.T) {
	to := (*TransferOutput)(nil)
	if err := to.Verify(); err == nil {
		t.Fatalf("TransferOutput.Verify should have errored on nil")
	}
}

func TestTransferOutputLargePayload(t *testing.T) {
	to := TransferOutput{
		Payload: make([]byte, MaxPayloadSize+1),
	}
	if err := to.Verify(); err == nil {
		t.Fatalf("TransferOutput.Verify should have errored on too large of a payload")
	}
}

func TestTransferOutputInvalidSecp256k1Output(t *testing.T) {
	to := TransferOutput{
		OutputOwners: secp256k1fx.OutputOwners{
			Addrs: []ids.ShortID{
				ids.ShortEmpty,
				ids.ShortEmpty,
			},
		},
	}
	if err := to.Verify(); err == nil {
		t.Fatalf("TransferOutput.Verify should have errored on too large of a payload")
	}
}
