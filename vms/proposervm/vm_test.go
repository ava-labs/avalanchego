package proposervm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type TestConnectorProVM struct {
	block.TestVM
}

func (vm *TestConnectorProVM) Connected(validatorID ids.ShortID) (bool, error) {
	return false, nil
}

func (vm *TestConnectorProVM) Disconnected(validatorID ids.ShortID) (bool, error) {
	return true, nil
}

func TestProposerVMConnectorHandling(t *testing.T) {
	// setup
	noConnectorVM := &block.TestVM{}
	proVM := VM{
		ChainVM: noConnectorVM,
	}

	// test
	_, err := proVM.Connected(ids.ShortID{})
	if err != ErrInnerVMNotConnector {
		t.Fatal("Proposer VM should signal that it wraps a ChainVM not implementing Connector interface with ErrInnerVMNotConnector error")
	}

	_, err = proVM.Disconnected(ids.ShortID{})
	if err != ErrInnerVMNotConnector {
		t.Fatal("Proposer VM should signal that it wraps a ChainVM not implementing Connector interface with ErrInnerVMNotConnector error")
	}

	// setup
	proConnVM := TestConnectorProVM{
		TestVM: block.TestVM{},
	}

	// test
	_, err = proConnVM.Connected(ids.ShortID{})
	if err != nil {
		t.Fatal("Proposer VM should forward wrapped Connection state if this implements Connector interface")
	}

	_, err = proConnVM.Disconnected(ids.ShortID{})
	if err != nil {
		t.Fatal("Proposer VM should forward wrapped Disconnection state if this implements Connector interface")
	}
}
