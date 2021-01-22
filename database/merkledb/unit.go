package merkledb

// UnitSize reflects the 16 possible values in a hex
// as a consequence each BranchNode will have 16 positions to store a Node.
// Given that a BranchNode can have the same Address of a LeafNode
// we add 1 position for that single exception.
// In position 17 or BranchNode[16] when theres a Node, it's a LeafNode.
const UnitSize = 16 + 1

type Unit byte

type Key []Unit

// FirstNonPrefix That function is only used in the BranchNode and it's purpose is to find the Node position in the BranchNode.
// It figures out the termination Unit -> position in the array.
// If the length of both keys are the same, the Node Position is the b.Nodes[UnitSize], the last position of the array.
// That is the exception for when we have BranchNode.SharedAddress=AAA1 and a LeafNode.Key=AAA1.
func FirstNonPrefix(baseKey Key, otherKey Key) Unit {
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

// SharedPrefix returns the minimum Key shared between two Key instances
// addr1 - ABC123
// addr2 - ABC567
// returns ABC
func SharedPrefix(key1, key2 Key) Key {
	smaller := key1
	larger := key2

	if len(key1) > len(key2) {
		smaller = key2
		larger = key1
	}

	p := 0
	for ; p < len(smaller); p++ {
		if smaller[p] != larger[p] {
			break
		}
	}
	return larger[:p]
}

// Equals returns whether the two Key are equal
func (k Key) Equals(otherKey Key) bool {
	if len(k) != len(otherKey) {
		return false
	}
	for i, v := range k {
		if v != otherKey[i] {
			return false
		}
	}

	return true
}

// BytesToKey converts a []byte to Key
// hardcoded behaviour of taking the first 4 bits and inserting them in a Unit
// and doing the same for the second set of 4 bits in a byte
func BytesToKey(bs []byte) Key {
	units := make(Key, 0, 2*len(bs))
	for _, n := range bs {
		units = append(units, Unit(n>>4))
		units = append(units, Unit(n%16))
	}
	return units
}

// IsPrefixed checks if prefix has a prefix in u
// u - 01234, 012,  001, 01
// p - 01   , 012 , 02 , 012
// = - T    , T   , F  , T
func IsPrefixed(prefix Key, u Key) bool {
	for i := 0; i < len(prefix) && i < len(u); i++ {
		if prefix[i] != u[i] {
			return false
		}
	}
	return true
}

// ContainsPrefix checks if key is prefixed in otherkey
// k - 01234, 012,  001, 01
// o - 01   , 012 , 02 , 012
// = - T    , T   , F  , F
func (k Key) ContainsPrefix(otherKey Key) bool {
	if len(otherKey) > len(k) {
		return false
	}
	return IsPrefixed(k, otherKey)
}

// ToBytes converts a key to a byte slice
func (k Key) ToBytes() []byte {
	length := len(k)
	if len(k) != 0 {
		k = append(k, Unit(0))
	}

	buf := make([]byte, 0, length)
	for i := 0; i < length; i += 2 {
		b := byte(k[i]<<4) + byte(k[i+1])
		buf = append(buf, b)
	}

	return buf
}

// ToExpandedBytes converts key to a byte slice that's a direct array conversion
// specially useful for key storing
func (k Key) ToExpandedBytes() []byte {
	length := len(k)
	buf := make([]byte, 0, length)

	for i := 0; i < length; i++ {
		buf = append(buf, byte(k[i]))
	}

	return buf
}

// Greater returns if k1 is greater than k2
func Greater(k1 Key, k2 Key) bool {
	for i := 0; i < len(k1) && i < len(k2); i++ {
		if k1[i] < k2[i] {
			return false
		}
	}

	return true
}
