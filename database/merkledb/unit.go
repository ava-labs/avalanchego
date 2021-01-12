package merkledb

const UnitSize = 16 + 1

type Unit byte

// FirstNonPrefix returns the first Unit that's not common between baseKey and otherKey
func FirstNonPrefix(baseKey []Unit, otherKey []Unit) Unit {
	smaller := otherKey
	larger := baseKey

	// the UnitSize - 1 position is the termination position
	// that position will only have LeafNodes
	if len(baseKey) == len(otherKey) {
		return Unit(UnitSize - 1)
	}

	if len(smaller) > len(larger) {
		smaller = baseKey
		larger = otherKey
	}

	return larger[len(smaller)]
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

// EqualUnits returns whether the two []Unit are equal
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

// FromBytes converts a []byte to []Unit
func FromBytes(bs []byte) []Unit {
	units := make([]Unit, 0, len(bs))
	for _, n := range bs {
		units = append(units, FromByte(n)...)
	}
	return units
}

// FromByte converts a byte to Unit
// in this case a Unit is a Nibble with means byte -> [2]Unit
func FromByte(b byte) []Unit {
	return []Unit{
		Unit(b >> 4),
		Unit(b % 16),
	}
}

// IsPrefixed checks if prefix is prefixed in u
// u - 01234, 012,  001, 01
// p - 01   , 012 , 02 , 012
// = - T    , T   , F  , T
func IsPrefixed(prefix []Unit, u []Unit) bool {
	for i := 0; i < len(prefix) && i < len(u); i++ {
		if prefix[i] != u[i] {
			return false
		}
	}

	return true
}

// ToBytes converts a slice of nibbles to a byte slice
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

// ToExpandedBytes converts a slice of nibbles to a byte slice that's a direct array conversion
// specially useful for key storing
func ToExpandedBytes(u []Unit) []byte {
	length := len(u)
	buf := make([]byte, 0, length)

	for i := 0; i < length; i++ {
		buf = append(buf, byte(u[i]))
	}

	return buf
}

// Greater returns if u1 is greater than u2
func Greater(u1 []Unit, u2 []Unit) bool {
	for i := 0; i < len(u1) && i < len(u2); i++ {
		if u1[i] < u2[i] {
			return false
		}
	}

	return true
}

// FromStorageKey converts StorageKeys in Keys
// removes the appended "B-" or "L-"
func FromStorageKey(u []Unit) []Unit {
	return u[2:]
}
