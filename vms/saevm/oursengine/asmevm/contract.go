package asmevm

import "github.com/holiman/uint256"

// Contract is one executing code frame.
type Contract struct {
	caller   Address
	self     Address
	codeAddr Address // where Code came from (differs under DELEGATECALL/CALLCODE)
	value    uint256.Int

	Code     []byte
	CodeHash Hash
	Input    []byte
	Gas      uint64

	analysis bitvec   // set bit = PUSH data byte, not a valid jumpdest
	dec      *decoded // asm core stream (nil until runAsm needs it)
}

func (c *Contract) useGas(gas uint64) bool {
	if c.Gas < gas {
		return false
	}
	c.Gas -= gas
	return true
}

func (c *Contract) validJumpdest(dest *uint256.Int) bool {
	udest, overflow := dest.Uint64WithOverflow()
	if overflow || udest >= uint64(len(c.Code)) {
		return false
	}
	if OpCode(c.Code[udest]) != JUMPDEST {
		return false
	}
	return !c.analysis.isSet(uint(udest))
}

// bitvec marks PUSH data bytes so they are excluded as jump destinations.
type bitvec []byte

func (b bitvec) isSet(pos uint) bool { return b[pos/8]&(1<<(pos%8)) != 0 }

// codeBitmap returns a bitvec where a set bit marks a byte that is PUSH data.
func codeBitmap(code []byte) bitvec {
	bits := make(bitvec, len(code)/8+1+4)
	for pc := 0; pc < len(code); {
		op := OpCode(code[pc])
		pc++
		if op < PUSH1 || op > PUSH32 {
			continue
		}
		n := int(op - PUSH1 + 1)
		for i := 0; i < n && pc < len(code); i, pc = i+1, pc+1 {
			bits[pc/8] |= 1 << (uint(pc) % 8)
		}
	}
	return bits
}
