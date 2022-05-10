// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"strconv"
	"strings"
)

var (
	DefaultParser            = NewParser(defaultVersionPrefix, defaultVersionSeparator)
	DefaultApplicationParser = NewApplicationParser(defaultAppSeparator, defaultVersionSeparator)
)

// Parser defines the interface of a Version parser
type Parser interface {
	Parse(string) (Semantic, error)
}

type parser struct {
	prefix    string
	separator string
}

func NewParser(prefix, separator string) Parser {
	return &parser{
		prefix:    prefix,
		separator: separator,
	}
}

func (p *parser) Parse(s string) (Semantic, error) {
	if !strings.HasPrefix(s, p.prefix) {
		return Semantic{}, fmt.Errorf("version string: %s missing required prefix: %s", s, p.prefix)
	}

	splitVersion := strings.SplitN(strings.TrimPrefix(s, p.prefix), p.separator, 3)
	if len(splitVersion) != 3 {
		return Semantic{}, fmt.Errorf("failed to parse %s as a version", s)
	}

	major, err := strconv.Atoi(splitVersion[0])
	if err != nil {
		return Semantic{}, fmt.Errorf("failed to parse %s as a version due to %w", s, err)
	}

	minor, err := strconv.Atoi(splitVersion[1])
	if err != nil {
		return Semantic{}, fmt.Errorf("failed to parse %s as a version due to %w", s, err)
	}

	patch, err := strconv.Atoi(splitVersion[2])
	if err != nil {
		return Semantic{}, fmt.Errorf("failed to parse %s as a version due to %w", s, err)
	}

	return NewSemantic(
		major,
		minor,
		patch,
		p.prefix,
		p.separator,
	), nil
}

// ApplicationParser defines the interface of an ApplicationVersion parser
type ApplicationParser struct {
	appSeparator  string
	versionParser parser
}

// NewApplicationParser returns a new parser
func NewApplicationParser(appSeparator string, versionSeparator string) ApplicationParser {
	return ApplicationParser{
		appSeparator: appSeparator,
		versionParser: parser{
			prefix:    "",
			separator: versionSeparator,
		},
	}
}

func (p ApplicationParser) Parse(s string) (Application, error) {
	splitApp := strings.SplitN(s, p.appSeparator, 2)
	if len(splitApp) != 2 {
		return Application{}, fmt.Errorf("failed to parse %s as a version", s)
	}

	version, err := p.versionParser.Parse(splitApp[1])
	if err != nil {
		return Application{}, err
	}

	return NewApplication(
		splitApp[0],
		p.appSeparator,
		p.versionParser.separator,
		version.Major,
		version.Minor,
		version.Patch,
	), nil
}
