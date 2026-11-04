package asmevm

import "github.com/holiman/uint256"

// createAddress = keccak256(rlp([sender, nonce]))[12:].
func createAddress(sender Address, nonce uint64) Address {
	enc := rlpAddrNonce(sender, nonce)
	h := Keccak256(enc)
	return Address(h[12:])
}

// create2Address = keccak256(0xff ++ sender ++ salt ++ keccak256(code))[12:].
func create2Address(sender Address, salt *uint256.Int, code []byte) Address {
	codeHash := Keccak256(code)
	saltB := salt.Bytes32()
	buf := make([]byte, 1+20+32+32)
	buf[0] = 0xff
	copy(buf[1:21], sender[:])
	copy(buf[21:53], saltB[:])
	copy(buf[53:85], codeHash[:])
	h := Keccak256(buf)
	return Address(h[12:])
}

// rlpAddrNonce RLP-encodes a list of [20-byte address, nonce].
func rlpAddrNonce(addr Address, nonce uint64) []byte {
	// address: 0x94 ++ 20 bytes
	var body []byte
	body = append(body, 0x94)
	body = append(body, addr[:]...)
	body = append(body, rlpUint(nonce)...)
	// list header
	out := make([]byte, 0, len(body)+1)
	out = append(out, 0xc0+byte(len(body)))
	out = append(out, body...)
	return out
}

func rlpUint(v uint64) []byte {
	if v == 0 {
		return []byte{0x80}
	}
	if v < 0x80 {
		return []byte{byte(v)}
	}
	var b [8]byte
	i := 8
	for v > 0 {
		i--
		b[i] = byte(v)
		v >>= 8
	}
	n := 8 - i
	out := make([]byte, 0, n+1)
	out = append(out, 0x80+byte(n))
	out = append(out, b[i:]...)
	return out
}
