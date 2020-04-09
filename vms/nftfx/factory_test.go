package nftfx

import (
	"testing"
)

func TestFactory(t *testing.T) {
	factory := Factory{}
	if fx := factory.New(); fx == nil {
		t.Fatalf("Factory.New returned nil")
	}
}
