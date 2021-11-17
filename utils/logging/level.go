// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"encoding/json"
	"fmt"
	"strings"
)

const alignedStringLen = 5

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

// Inverse of Level.String()
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
// The returned value has length [alignedStringLen].
func (l Level) AlignedString() string {
	s := l.String()
	sLen := len(s)
	switch {
	case sLen < alignedStringLen:
		// Pad with spaces on the right
		return fmt.Sprintf("%s%s", s, strings.Repeat(" ", alignedStringLen-sLen))
	case sLen == alignedStringLen:
		return s
	default:
		return s[:alignedStringLen]
	}
}

func (l Level) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

func (l *Level) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	var err error
	*l, err = ToLevel(str)
	return err
}
