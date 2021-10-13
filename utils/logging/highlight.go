// (c) 2020, Alex Willmer, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh/terminal"
)

// Highlighting modes available
const (
	Plain Highlight = iota
	Colors
)

var errUnknownHighlight = errors.New("unknown highlight")

// Highlight mode to apply to displayed logs
type Highlight int

// ToHighlight chooses a highlighting mode
func ToHighlight(h string, fd uintptr) (Highlight, error) {
	switch strings.ToUpper(h) {
	case "PLAIN":
		return Plain, nil
	case "COLORS":
		return Colors, nil
	case "AUTO":
		if !terminal.IsTerminal(int(fd)) {
			return Plain, nil
		}
		return Colors, nil
	default:
		return Plain, fmt.Errorf("unknown highlight mode: %s", h)
	}
}

func (h *Highlight) MarshalJSON() ([]byte, error) {
	switch *h {
	case Plain:
		return []byte("\"PLAIN\""), nil
	case Colors:
		return []byte("\"COLORS\""), nil
	default:
		return nil, errUnknownHighlight
	}
}
