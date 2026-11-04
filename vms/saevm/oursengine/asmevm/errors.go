package asmevm

import (
	"errors"
	"fmt"
)

var (
	ErrOutOfGas                 = errors.New("out of gas")
	ErrStackUnderflow           = errors.New("stack underflow")
	ErrStackOverflow            = errors.New("stack overflow")
	ErrInvalidJump              = errors.New("invalid jump destination")
	ErrExecutionReverted        = errors.New("execution reverted")
	ErrInvalidOpcode            = errors.New("invalid opcode")
	ErrGasUintOverflow          = errors.New("gas uint64 overflow")
	ErrWriteProtection          = errors.New("write protection")
	ErrReturnDataOutOfBounds    = errors.New("return data out of bounds")
	ErrDepth                    = errors.New("max call depth exceeded")
	ErrInsufficientBalance      = errors.New("insufficient balance for transfer")
	ErrContractAddressCollision = errors.New("contract address collision")
	ErrMaxCodeSizeExceeded      = errors.New("max code size exceeded")
	ErrMaxInitCodeSizeExceeded  = errors.New("max initcode size exceeded")
	ErrInvalidCode              = errors.New("invalid code: must not begin with 0xef")
	ErrNonceUintOverflow        = errors.New("nonce uint64 overflow")
	ErrCodeStoreOutOfGas        = errors.New("contract creation code storage out of gas")

	// errStopToken is not an error: it unwinds the frame on STOP/RETURN.
	errStopToken = errors.New("stop token")
)

// haltError is a halt with geth's detail in its message (invalid opcode, stack under- and
// overflow, as libevm's vm.ErrInvalidOpCode, ErrStackUnderflow, ErrStackOverflow word them); it
// Is its sentinel. Only the error path builds one.
type haltError struct {
	sentinel error
	msg      string
}

func (e *haltError) Error() string        { return e.msg }
func (e *haltError) Is(target error) bool { return target == e.sentinel }

func errInvalidOp(op OpCode) error {
	name := fmt.Sprintf("opcode %#x not defined", int(op))
	if op == INVALID { // the one byte without an instruction that geth names
		name = "INVALID"
	}
	return &haltError{ErrInvalidOpcode, "invalid opcode: " + name}
}

func errUnderflow(have, need int) error {
	return &haltError{ErrStackUnderflow, fmt.Sprintf("stack underflow (%d <=> %d)", have, need)}
}

func errOverflow(have, limit int) error {
	return &haltError{ErrStackOverflow, fmt.Sprintf("stack limit reached %d (%d)", have, limit)}
}
