package platformvm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
)

func TestBaseTxSyntacticVerify(t *testing.T) {
	type test struct {
		tx        *BaseTx
		shouldErr bool
		errMsg    string
	}

	vm := defaultVM()
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	nilID := ids.NewID([32]byte{})
	nilID.ID = nil
	tests := []test{
		{
			nil,
			true,
			"tx is nil",
		},
		{
			&BaseTx{
				vm:           nil,
				BlockchainID: ids.Empty,
			},
			true,
			"vm is nil",
		},
		{
			&BaseTx{
				vm:           vm,
				BlockchainID: nilID,
			},
			true,
			"ID is nil",
		},
		{
			&BaseTx{
				vm:           vm,
				BlockchainID: ids.Empty,
				Memo:         make([]byte, maxMemoSize+1),
			},
			true,
			"memo is too long",
		},
		{
			&BaseTx{
				vm:           vm,
				BlockchainID: ids.Empty,
				Memo:         make([]byte, maxMemoSize),
			},
			false,
			"",
		},
	}

	for _, test := range tests {
		if err := test.tx.SyntacticVerify(); err == nil && test.shouldErr {
			t.Errorf("expected error because '%s' but got none", test.errMsg)
		} else if err != nil && !test.shouldErr {
			t.Errorf("expected no error but got %s", err)
		}
	}
}
