// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"strconv"
	"strings"
)

// Parser defines the interface of a version parser
type Parser interface {
	Parse(string) (Version, error)
}

type parser struct {
	appSeparator     string
	versionSeparator string
}

// NewDefaultParser returns a new parser with the default separators
func NewDefaultParser() Parser { return NewParser(defaultAppSeparator, defaultVersionSeparator) }

// NewParser returns a new parser
func NewParser(appSeparator string, versionSeparator string) Parser {
	return &parser{
		appSeparator:     appSeparator,
		versionSeparator: versionSeparator,
	}
}

func (p *parser) Parse(s string) (Version, error) {
	splitApp := strings.SplitN(s, p.appSeparator, 2)
	if len(splitApp) != 2 {
		return nil, fmt.Errorf("failed to parse %s as a version", s)
	}
	splitVersion := strings.SplitN(splitApp[1], p.versionSeparator, 3)
	if len(splitVersion) != 3 {
		return nil, fmt.Errorf("failed to parse %s as a version", s)
	}

	major, err := strconv.Atoi(splitVersion[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s as a version due to %w", s, err)
	}

	minor, err := strconv.Atoi(splitVersion[1])
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s as a version due to %w", s, err)
	}

	patch, err := strconv.Atoi(splitVersion[2])
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s as a version due to %w", s, err)
	}

	return NewVersion(
		splitApp[0],
		p.appSeparator,
		p.versionSeparator,
		major,
		minor,
		patch,
	), nil
}
