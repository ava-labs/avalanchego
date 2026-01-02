// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

import (
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap/zapcore"
	"golang.org/x/term"
)

// Format modes available
const (
	Auto Format = iota
	Plain
	Colors
	JSON

	AutoString   = "auto"
	PlainString  = "plain"
	ColorsString = "colors"
	JSONString   = "json"

	FormatDescription = "The structure of log format. Defaults to 'auto' which formats terminal-like logs, when the output is a terminal. Otherwise, should be one of {auto, plain, colors, json}"

	termTimeFormat = "[01-02|15:04:05.000]"
)

var (
	formatJSON = []string{
		`"auto"`,
		`"plain"`,
		`"colors"`,
		`"json"`,
	}

	errUnknownFormat = errors.New("unknown format")

	defaultEncoderConfig = zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	jsonEncoderConfig zapcore.EncoderConfig

	termTimeEncoder = zapcore.TimeEncoderOfLayout(termTimeFormat)
)

func init() {
	jsonEncoderConfig = defaultEncoderConfig
	jsonEncoderConfig.EncodeLevel = jsonLevelEncoder
	jsonEncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	jsonEncoderConfig.EncodeDuration = zapcore.NanosDurationEncoder
}

// Highlight mode to apply to displayed logs
type Format int

// ToFormat chooses a highlighting mode
func ToFormat(h string, fd uintptr) (Format, error) {
	switch strings.ToLower(h) {
	case AutoString:
		if !term.IsTerminal(int(fd)) {
			return Plain, nil
		}
		return Colors, nil
	case PlainString:
		return Plain, nil
	case ColorsString:
		return Colors, nil
	case JSONString:
		return JSON, nil
	default:
		return Plain, fmt.Errorf("unknown format mode: %s", h)
	}
}

func (f Format) MarshalJSON() ([]byte, error) {
	if f < 0 || int(f) >= len(formatJSON) {
		return nil, errUnknownFormat
	}
	return []byte(formatJSON[f]), nil
}

func (f Format) WrapPrefix(prefix string) string {
	if prefix == "" || f == JSON {
		return prefix
	}
	return fmt.Sprintf("<%s>", prefix)
}

func (f Format) ConsoleEncoder() zapcore.Encoder {
	switch f {
	case Colors:
		return zapcore.NewConsoleEncoder(newTermEncoderConfig(ConsoleColorLevelEncoder))
	case JSON:
		return zapcore.NewJSONEncoder(jsonEncoderConfig)
	default:
		return zapcore.NewConsoleEncoder(newTermEncoderConfig(levelEncoder))
	}
}

func (f Format) FileEncoder() zapcore.Encoder {
	switch f {
	case JSON:
		return zapcore.NewJSONEncoder(jsonEncoderConfig)
	default:
		return zapcore.NewConsoleEncoder(newTermEncoderConfig(levelEncoder))
	}
}

func newTermEncoderConfig(lvlEncoder zapcore.LevelEncoder) zapcore.EncoderConfig {
	config := defaultEncoderConfig
	config.EncodeLevel = lvlEncoder
	config.EncodeTime = termTimeEncoder
	config.ConsoleSeparator = " "
	return config
}

func levelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(Level(l).String())
}

func jsonLevelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(Level(l).LowerString())
}

func ConsoleColorLevelEncoder(l zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	s, ok := levelToCapitalColorString[Level(l)]
	if !ok {
		s = unknownLevelColor.Wrap(l.String())
	}
	enc.AppendString(s)
}
