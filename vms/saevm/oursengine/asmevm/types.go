package asmevm

import "github.com/holiman/uint256"

// Address is a 20-byte account address.
type Address [20]byte

// Hash is a 32-byte value.
type Hash [32]byte

func (a Address) hash() Hash { var h Hash; copy(h[12:], a[:]); return h }

// Stack holds EVM words. The assembly reads and writes data directly; data is
// kept at capacity 1024 and resliced to the live height around host calls.
type Stack struct {
	data []uint256.Int
}

func (s *Stack) len() int            { return len(s.data) }
func (s *Stack) push(v *uint256.Int) { s.data = append(s.data, *v) }
func (s *Stack) pop() uint256.Int {
	v := s.data[len(s.data)-1]
	s.data = s.data[:len(s.data)-1]
	return v
}
func (s *Stack) peek() *uint256.Int      { return &s.data[len(s.data)-1] }
func (s *Stack) back(n int) *uint256.Int { return &s.data[len(s.data)-1-n] }

// Memory is the frame's byte buffer. lastGasCost is the geth memory cost of the
// current length, used to charge only the delta on expansion.
type Memory struct {
	store       []byte
	lastGasCost uint64
}

func (m *Memory) len() int { return len(m.store) }

// resize grows the buffer to size bytes, zeroing new space.
func (m *Memory) resize(size uint64) {
	if uint64(len(m.store)) < size {
		m.store = append(m.store, make([]byte, size-uint64(len(m.store)))...)
	}
}

// swap exchanges the top with the n-th element below it (n>=1).
func (s *Stack) swap(n int) {
	l := len(s.data)
	s.data[l-1], s.data[l-1-n] = s.data[l-1-n], s.data[l-1]
}

// dup pushes a copy of the n-th element from the top (n>=1).
func (s *Stack) dup(n int) {
	s.data = append(s.data, s.data[len(s.data)-n])
}

// pushN appends v by value.
func (s *Stack) pushN(v uint256.Int) { s.data = append(s.data, v) }

// getCopy returns a copy of size bytes at offset (zero-extended by resize).
func (m *Memory) getCopy(offset, size uint64) []byte {
	if size == 0 {
		return nil
	}
	out := make([]byte, size)
	copy(out, m.store[offset:offset+size])
	return out
}

// getPtr returns a sub-slice aliasing the buffer at [offset, offset+size).
func (m *Memory) getPtr(offset, size uint64) []byte {
	if size == 0 {
		return nil
	}
	return m.store[offset : offset+size]
}

// set writes val at [offset, offset+size) (buffer must already be large enough).
func (m *Memory) set(offset, size uint64, val []byte) {
	if size > 0 {
		copy(m.store[offset:offset+size], val)
	}
}

// set32 writes the 32-byte big-endian val at offset.
func (m *Memory) set32(offset uint64, val *uint256.Int) {
	b := val.Bytes32()
	copy(m.store[offset:offset+32], b[:])
}
