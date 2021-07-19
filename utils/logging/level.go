// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"errors"
	"fmt"
	"strings"
)

var errMissingQuotes = errors.New("first and last characters should be quotes")

type Level int

const (
	Off Level = iota
	Fatal
	Error
	Warn
	Info
	Trace
	Debug
	Verbo
)

const (
	fatalStr   = "FATAL"
	errorStr   = "ERROR"
	warnStr    = "WARN"
	infoStr    = "INFO"
	traceStr   = "TRACE"
	debugStr   = "DEBUG"
	verboStr   = "VERBO"
	offStr     = "OFF"
	unknownStr = "UNKNO"
)

func ToLevel(l string) (Level, error) {
	switch strings.ToUpper(l) {
	case offStr:
		return Off, nil
	case fatalStr:
		return Fatal, nil
	case errorStr:
		return Error, nil
	case warnStr:
		return Warn, nil
	case infoStr:
		return Info, nil
	case traceStr:
		return Trace, nil
	case debugStr:
		return Debug, nil
	case verboStr:
		return Verbo, nil
	default:
		return Off, fmt.Errorf("unknown log level: %q", l)
	}
}

func (l Level) Color() Color {
	switch l {
	case Fatal:
		return Red
	case Error:
		return Orange
	case Warn:
		return Yellow
	case Info:
		// Rather than using white, use the default to better support terminals
		// with a white background.
		return Reset
	case Trace:
		return LightPurple
	case Debug:
		return LightBlue
	case Verbo:
		return LightGreen
	default:
		return Reset
	}
}

func (l Level) String() string {
	switch l {
	case Fatal:
		return fatalStr
	case Error:
		return errorStr
	case Warn:
		return warnStr
	case Info:
		return infoStr
	case Trace:
		return traceStr
	case Debug:
		return debugStr
	case Verbo:
		return verboStr
	case Off:
		return offStr
	default:
		// This should never happen
		return unknownStr
	}
}

// String representation of this level as it will appear
// in log files and in logs displayed to screen.
// The returned value has length 5.
func (l Level) AlignedString() string {
	s := unknownStr
	// Should always match a case below
	// Note that Off is not included because by definition
	// we don't log/display when the [l] is Off.
	switch l {
	case Fatal:
		s = fatalStr
	case Error:
		s = errorStr
	case Warn:
		s = warnStr
	case Info:
		s = infoStr
	case Trace:
		s = traceStr
	case Debug:
		s = debugStr
	case Verbo:
		s = verboStr
	}
	return to5Chars(s)
}

func (l Level) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", l)), nil
}

func (l *Level) UnmarshalJSON(b []byte) error {
	str := string(b)
	if len(str) < 2 {
		return errMissingQuotes
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}

	str = strings.ToUpper(str[1:lastIndex])
	var err error
	*l, err = ToLevel(str)
	return err
}

// If len([s]) < 5, returns [s] padded with spaces at the end
// If len([s]) == 5, returns [s]
// If len([s]), returns the first 5 characters of [s]
func to5Chars(s string) string {
	l := len(s)
	switch {
	case l < 5:
		return fmt.Sprintf("%-5s", s)
	case l == 5:
		return s
	default:
		return s[:5]
	}
}
