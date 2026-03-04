// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package logging

type Color string

// Colors
const (
	Black       Color = "\033[0;30m"
	DarkGray    Color = "\033[1;30m"
	Red         Color = "\033[0;31m"
	LightRed    Color = "\033[1;31m"
	Green       Color = "\033[0;32m"
	LightGreen  Color = "\033[1;32m"
	Orange      Color = "\033[0;33m"
	Yellow      Color = "\033[1;33m"
	Blue        Color = "\033[0;34m"
	LightBlue   Color = "\033[1;34m"
	Purple      Color = "\033[0;35m"
	LightPurple Color = "\033[1;35m"
	Cyan        Color = "\033[0;36m"
	LightCyan   Color = "\033[1;36m"
	LightGray   Color = "\033[0;37m"
	White       Color = "\033[1;37m"

	Reset   Color = "\033[0;0m"
	Bold    Color = "\033[;1m"
	Reverse Color = "\033[;7m"
)

var (
	levelToColor = map[Level]Color{
		Fatal: Red,
		Error: Orange,
		Warn:  Yellow,
		// Rather than using white, use the default to better support terminals
		// with a white background.
		Info:  Reset,
		Trace: LightPurple,
		Debug: LightBlue,
		Verbo: LightGreen,
	}

	levelToCapitalColorString = make(map[Level]string, len(levelToColor))
	unknownLevelColor         = Reset
)

func (lc Color) Wrap(text string) string {
	return string(lc) + text + string(Reset)
}

func init() {
	for level, color := range levelToColor {
		levelToCapitalColorString[level] = color.Wrap(level.String())
	}
}
