package platformvm

import (
	"errors"

	"github.com/ava-labs/gecko/vms/components/avax"
)

var (
	errInvalidLocktime = errors.New("invalid locktime")
)

// StakeableLockOut ...
type StakeableLockOut struct {
	Locktime             uint64 `serialize:"true" json:"locktime"`
	avax.TransferableOut `serialize:"true"`
}

// Verify ...
func (s *StakeableLockOut) Verify() error {
	if s.Locktime == 0 {
		return errInvalidLocktime
	}
	if _, nested := s.TransferableOut.(*StakeableLockOut); nested {
		return errors.New("shouldn't nest stakeable locks")
	}
	return s.TransferableOut.Verify()
}

// StakeableLockIn ...
type StakeableLockIn struct {
	Locktime            uint64 `serialize:"true" json:"locktime"`
	avax.TransferableIn `serialize:"true"`
}

// Verify ...
func (s *StakeableLockIn) Verify() error {
	if s.Locktime == 0 {
		return errInvalidLocktime
	}
	if _, nested := s.TransferableIn.(*StakeableLockIn); nested {
		return errors.New("shouldn't nest stakeable locks")
	}
	return s.TransferableIn.Verify()
}
