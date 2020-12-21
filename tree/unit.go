package tree

const UnitSize = 16

type Unit byte

func FirstNonPrefix(basekey []Unit, otherKey []Unit) Unit {
	return otherKey[len(basekey)]
}

// SharedPrefix returns the minimum Unit shared between two Unit
// addr1 - ABC123
// addr2 - ABC567
// returns ABC
func SharedPrefix(address1 []Unit, address2 []Unit) []Unit {

	shared := []Unit{}
	smaller := address1
	larger := address2

	if len(address1) > len(address2) {
		smaller = address2
		larger = address1
	}

	for i, v := range smaller {
		if v != larger[i] {
			break
		}
		shared = append(shared, v)
	}
	return shared
}

func EqualUnits(key []Unit, key2 []Unit) bool {
	if len(key) != len(key2) {
		return false
	}
	for i, v := range key {
		if v != key2[i] {
			return false
		}
	}

	return true
}

func FromBytes(bs []byte) []Unit {
	units := make([]Unit, 0, len(bs))
	for _, n := range bs {
		units = append(units, FromByte(n)...)
	}
	return units
}

func FromByte(b byte) []Unit {
	return []Unit{
		Unit(b >> 4),
		Unit(b % 16),
	}
}

// TODO Review this padding
// ToBytes converts a slice of nibbles to a byte slice
// assuming the nibble slice has even number of nibbles.
func ToBytes(u []Unit) []byte {
	length := len(u)
	if len(u) != 0 {
		u = append(u, Unit(0))
	}
	buf := make([]byte, 0, length)

	for i := 0; i < length; i += 2 {
		b := byte(u[i]<<4) + byte(u[i+1])
		buf = append(buf, b)
	}

	return buf
}
