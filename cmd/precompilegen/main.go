// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2016 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/internal/flags"
	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli/v2"
)

var (
	// Git SHA1 commit hash of the release (set via linker flags)
	gitCommit = ""
	gitDate   = ""

	app *cli.App
)

var (
	// Flags needed by abigen
	abiFlag = &cli.StringFlag{
		Name:  "abi",
		Usage: "Path to the Ethereum contract ABI json to bind, - for STDIN",
	}
	typeFlag = &cli.StringFlag{
		Name:  "type",
		Usage: "Struct name for the precompile (default = ABI name)",
	}
	pkgFlag = &cli.StringFlag{
		Name:  "pkg",
		Usage: "Package name to generate the precompile into (default = precompile)",
	}
	outFlag = &cli.StringFlag{
		Name:  "out",
		Usage: "Output file for the generated precompile (default = STDOUT)",
	}
)

func init() {
	app = flags.NewApp(gitCommit, gitDate, "subnet-evm precompile generator tool")
	app.Name = "precompilegen"
	app.Flags = []cli.Flag{
		abiFlag,
		outFlag,
		pkgFlag,
		typeFlag,
	}
	app.Action = precompilegen
}

func precompilegen(c *cli.Context) error {
	if !c.IsSet(outFlag.Name) && !c.IsSet(typeFlag.Name) {
		utils.Fatalf("type (--type) should be set explicitly for STDOUT ")
	}
	pkg := pkgFlag.Name
	if pkg == "" {
		pkg = "precompile"
	}
	lang := bind.LangGo
	// If the entire solidity code was specified, build and bind based on that
	var (
		abis    []string
		bins    []string
		types   []string
		sigs    []map[string]string
		libs    = make(map[string]string)
		aliases = make(map[string]string)
	)
	if c.String(abiFlag.Name) == "" {
		utils.Fatalf("no abi path is specified (--abi)")
	}
	// Load up the ABI
	var (
		abi []byte
		err error
	)
	input := c.String(abiFlag.Name)
	if input == "-" {
		abi, err = io.ReadAll(os.Stdin)
	} else {
		abi, err = os.ReadFile(input)
	}
	if err != nil {
		utils.Fatalf("Failed to read input ABI: %v", err)
	}
	abis = append(abis, string(abi))
	bins = append(bins, "")
	kind := c.String(typeFlag.Name)
	if kind == "" {
		fn := filepath.Base(input)
		kind = strings.TrimSuffix(fn, filepath.Ext(fn))
		kind = strings.TrimSpace(kind)
	}
	types = append(types, kind)

	// Generate the contract precompile
	code, err := bind.Bind(types, abis, bins, sigs, pkg, lang, libs, aliases, true)
	if err != nil {
		utils.Fatalf("Failed to generate ABI precompile: %v", err)
	}

	// Either flush it out to a file or display on the standard output
	if !c.IsSet(outFlag.Name) {
		fmt.Printf("%s\n", code)
		return nil
	}

	if err := os.WriteFile(c.String(outFlag.Name), []byte(code), 0o600); err != nil {
		utils.Fatalf("Failed to write ABI precompile: %v", err)
	}
	return nil
}

func main() {
	log.Root().SetHandler(log.LvlFilterHandler(log.LvlInfo, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
