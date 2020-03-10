// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

// ID is an identifier for a codec
type ID uint32

// Codec types
const (
	NoID ID = iota
	GenericID
	CustomID
)

// Verify that the codec is a known codec value. Returns nil if the codec is
// valid.
func (c ID) Verify() error {
	switch c {
	case NoID, GenericID, CustomID:
		return nil
	default:
		return errBadCodec
	}
}

func (c ID) String() string {
	switch c {
	case NoID:
		return "No Codec"
	case GenericID:
		return "Generic Codec"
	case CustomID:
		return "Custom Codec"
	default:
		return "Unknown Codec"
	}
}
