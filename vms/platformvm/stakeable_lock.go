package platformvm

import (
	"errors"

	"github.com/ava-labs/gecko/vms/components/ava"
)

// StakeableLock ...
type StakeableLock struct {
	Locktime            uint64 `serialize:"true"`
	ava.TransferableOut `serialize:"true"`
}

// Verify ...
func (s *StakeableLock) Verify() error {
	if _, nested := s.TransferableOut.(*StakeableLock); nested {
		return errors.New("shouldn't nest stakeable locks")
	}
	return s.TransferableOut.Verify()
}
