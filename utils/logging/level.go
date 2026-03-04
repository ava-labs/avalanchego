// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap/zapcore"
)

type Level zapcore.Level

const (
	Verbo Level = iota - 9
	Debug
	Trace
	Info
	Warn
	Error
	Fatal
	Off

	fatalStr   = "FATAL"
	errorStr   = "ERROR"
	warnStr    = "WARN"
	infoStr    = "INFO"
	traceStr   = "TRACE"
	debugStr   = "DEBUG"
	verboStr   = "VERBO"
	offStr     = "OFF"
	unknownStr = "UNKNO"

	fatalLowStr   = "fatal"
	errorLowStr   = "error"
	warnLowStr    = "warn"
	infoLowStr    = "info"
	traceLowStr   = "trace"
	debugLowStr   = "debug"
	verboLowStr   = "verbo"
	offLowStr     = "off"
	unknownLowStr = "unkno"
)

var ErrUnknownLevel = errors.New("unknown log level")

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
		return Off, fmt.Errorf("%w: %q", ErrUnknownLevel, l)
	}
}

func (l Level) String() string {
	switch l {
	case Off:
		return offStr
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
	default:
		// This should never happen
		return unknownStr
	}
}

func (l Level) LowerString() string {
	switch l {
	case Off:
		return offLowStr
	case Fatal:
		return fatalLowStr
	case Error:
		return errorLowStr
	case Warn:
		return warnLowStr
	case Info:
		return infoLowStr
	case Trace:
		return traceLowStr
	case Debug:
		return debugLowStr
	case Verbo:
		return verboLowStr
	default:
		// This should never happen
		return unknownLowStr
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
