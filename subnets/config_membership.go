// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

var (
	errMembershipCAWhenNotValidatorOnly = errors.New("membershipCAPath and membershipCAContent can only be set when ValidatorOnly is true")
	errTooManyMembershipCASources       = errors.New("only one of membershipCAPath or membershipCAContent can be set")
	errMembershipCRLWithoutMembershipCA = errors.New("membershipCRLPath and membershipCRLContent can only be set with membershipCAPath or membershipCAContent")
	errTooManyMembershipCRLSources      = errors.New("only one of membershipCRLPath or membershipCRLContent can be set")
)

// LoadMembershipCA parses MembershipCAPath or MembershipCAContent into the
// roots that a peer's certificate chain must verify against to be a member of
// this Subnet, and applies MembershipCRLPath or MembershipCRLContent to them.
// It returns nil when the Subnet declares no membership CA.
//
// [c] must have passed [Config.ValidParameters], which settles which keys may
// be set together; this only reads and parses them.
func (c *Config) LoadMembershipCA() (*MembershipCA, error) {
	if !c.hasMembershipCA() {
		return nil, nil
	}

	caPEM, err := readMembershipPEM(c.MembershipCAPath, c.MembershipCAContent)
	if err != nil {
		return nil, fmt.Errorf("reading membershipCAPath: %w", err)
	}
	if !c.hasMembershipCRL() {
		return ParseMembershipCA(caPEM)
	}
	crlPEM, err := readMembershipPEM(c.MembershipCRLPath, c.MembershipCRLContent)
	if err != nil {
		return nil, fmt.Errorf("reading membershipCRLPath: %w", err)
	}
	return ParseMembershipCAWithCRL(caPEM, crlPEM)
}

func (c *Config) hasMembershipCA() bool {
	return c.MembershipCAPath != "" || len(c.MembershipCAContent) > 0
}

func (c *Config) hasMembershipCRL() bool {
	return c.MembershipCRLPath != "" || len(c.MembershipCRLContent) > 0
}

// readMembershipPEM returns the PEM held by the file at [path], or by [inline]
// when [path] is empty.
func readMembershipPEM(path string, inline []string) ([]byte, error) {
	if path == "" {
		return []byte(strings.Join(inline, "\n")), nil
	}
	return os.ReadFile(path)
}

// validMembership is the part of [Config.ValidParameters] that covers the
// membership CA. It reads no file; [Config.LoadMembershipCA] does that.
func (c *Config) validMembership() error {
	switch {
	case c.hasMembershipCA() && !c.ValidatorOnly:
		return errMembershipCAWhenNotValidatorOnly
	case c.MembershipCAPath != "" && len(c.MembershipCAContent) > 0:
		return errTooManyMembershipCASources
	case c.MembershipCRLPath != "" && len(c.MembershipCRLContent) > 0:
		return errTooManyMembershipCRLSources
	// A list does nothing without the CA it is checked against, so it is
	// almost always a CA key that is missing or misspelled.
	case c.hasMembershipCRL() && !c.hasMembershipCA():
		return errMembershipCRLWithoutMembershipCA
	default:
		return nil
	}
}
