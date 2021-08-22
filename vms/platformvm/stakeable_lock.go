package platformvm

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var errInvalidLocktime = errors.New("invalid locktime")

type StakeableLockOut struct {
	Locktime             uint64 `serialize:"true" json:"locktime"`
	avax.TransferableOut `serialize:"true"`
}

func (s *StakeableLockOut) Addresses() [][]byte {
	if addressable, ok := s.TransferableOut.(avax.Addressable); ok {
		return addressable.Addresses()
	}
	return nil
}

func (s *StakeableLockOut) Verify() error {
	if s.Locktime == 0 {
		return errInvalidLocktime
	}
	if _, nested := s.TransferableOut.(*StakeableLockOut); nested {
		return errors.New("shouldn't nest stakeable locks")
	}
	return s.TransferableOut.Verify()
}

type StakeableLockIn struct {
	Locktime            uint64 `serialize:"true" json:"locktime"`
	avax.TransferableIn `serialize:"true"`
}

func (s *StakeableLockIn) Verify() error {
	if s.Locktime == 0 {
		return errInvalidLocktime
	}
	if _, nested := s.TransferableIn.(*StakeableLockIn); nested {
		return errors.New("shouldn't nest stakeable locks")
	}
	return s.TransferableIn.Verify()
}
