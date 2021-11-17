// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"strconv"
	"strings"
)

// Parser defines the interface of a Version parser
type Parser interface {
	Parse(string) (Version, error)
}

type parser struct {
	prefix    string
	separator string
}

func NewDefaultParser() Parser { return NewParser(defaultVersionPrefix, defaultVersionSeparator) }

func NewParser(prefix, separator string) Parser {
	return &parser{
		prefix:    prefix,
		separator: separator,
	}
}

func (p *parser) Parse(s string) (Version, error) {
	if !strings.HasPrefix(s, p.prefix) {
		return nil, fmt.Errorf("version string: %s missing required prefix: %s", s, p.prefix)
	}

	splitVersion := strings.SplitN(strings.TrimPrefix(s, p.prefix), p.separator, 3)
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
		major,
		minor,
		patch,
		p.prefix,
		p.separator,
	), nil
}

// ApplicationParser defines the interface of an ApplicationVersion parser
type ApplicationParser interface {
	Parse(string) (Application, error)
}

type applicationParser struct {
	appSeparator  string
	versionParser *parser
}

// NewDefaultApplicationParser returns a new parser with the default separators
func NewDefaultApplicationParser() ApplicationParser {
	return NewApplicationParser(defaultAppSeparator, defaultVersionSeparator)
}

// NewApplicationParser returns a new parser
func NewApplicationParser(appSeparator string, versionSeparator string) ApplicationParser {
	return &applicationParser{
		appSeparator: appSeparator,
		versionParser: &parser{
			prefix:    "",
			separator: versionSeparator,
		},
	}
}

func (p *applicationParser) Parse(s string) (Application, error) {
	splitApp := strings.SplitN(s, p.appSeparator, 2)
	if len(splitApp) != 2 {
		return nil, fmt.Errorf("failed to parse %s as a version", s)
	}

	version, err := p.versionParser.Parse(splitApp[1])
	if err != nil {
		return nil, err
	}

	return NewApplication(
		splitApp[0],
		p.appSeparator,
		p.versionParser.separator,
		version.Major(),
		version.Minor(),
		version.Patch(),
	), nil
}
