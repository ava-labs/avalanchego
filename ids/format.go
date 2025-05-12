package ids

import "fmt"

var _ = []fmt.Formatter{ID{}, ShortID{}}

// Format implements the [fmt.Formatter] interface.
func (id ID) Format(s fmt.State, verb rune) {
	format(s, verb, id)
}

// Format implements the [fmt.Formatter] interface.
func (id ShortID) Format(s fmt.State, verb rune) {
	format(s, verb, id)
}

// format implements the [fmt.Formatter] interface for [ID] and [ShortID].
func format[T interface {
	String() string
	Hex() string
}](s fmt.State, verb rune, id T) {
	switch verb {
	case 'x':
		if s.Flag('#') {
			s.Write([]byte("0x")) //nolint:errcheck // [fmt.Formatter] doesn't allow for returning errors, and the implementation of [fmt.State] always returns nil on Write()
		}
		s.Write([]byte(id.Hex())) //nolint:errcheck // See above

	case 'q':
		str := id.String()
		buf := make([]byte, len(str)+2)
		buf[0] = '"'
		buf[len(buf)-1] = '"'
		copy(buf[1:], str)
		s.Write(buf) //nolint:errcheck // See above

	default:
		s.Write([]byte(id.String())) //nolint:errcheck // See above
	}
}
