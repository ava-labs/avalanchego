// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
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

	_ "embed"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/accounts/abi/bind/precompilebind"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/internal/flags"
	"github.com/ava-labs/libevm/cmd/utils"
	"github.com/ava-labs/libevm/log"
	"github.com/urfave/cli/v2"
)

//go:embed template-readme.md
var readme string

var (
	// Flags needed by abigen
	abiFlag = &cli.StringFlag{
		Name:  "abi",
		Usage: "Path to the contract ABI json to generate, - for STDIN",
	}
	typeFlag = &cli.StringFlag{
		Name:  "type",
		Usage: "Struct name for the precompile (default = {abi file name})",
	}
	pkgFlag = &cli.StringFlag{
		Name:  "pkg",
		Usage: "Go package name to generate the precompile into (default = {type})",
	}
	outFlag = &cli.StringFlag{
		Name:  "out",
		Usage: "Output folder for the generated precompile files, - for STDOUT (default = ./precompile/contracts/{pkg}). Test files won't be generated if STDOUT is used",
	}
)

var app = flags.NewApp("subnet-evm precompile generator tool")

func init() {
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
	outFlagStr := c.String(outFlag.Name)
	isOutStdout := outFlagStr == "-"

	if isOutStdout && !c.IsSet(typeFlag.Name) {
		utils.Fatalf("type (--type) should be set explicitly for STDOUT ")
	}
	lang := bind.LangGo
	// If the entire solidity code was specified, build and bind based on that
	var (
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

	bins = append(bins, "")

	kind := c.String(typeFlag.Name)
	if kind == "" {
		fn := filepath.Base(input)
		kind = strings.TrimSuffix(fn, filepath.Ext(fn))
		kind = strings.TrimSpace(kind)
	}
	types = append(types, kind)

	pkg := c.String(pkgFlag.Name)
	if pkg == "" {
		pkg = strings.ToLower(kind)
	}

	if outFlagStr == "" {
		outFlagStr = filepath.Join("./precompile/contracts", pkg)
	}

	abifilename := ""
	abipath := ""
	// we should not generate the abi file if output is set to stdout
	if !isOutStdout {
		// get file name from the output path
		abifilename = "contract.abi"
		abipath = filepath.Join(outFlagStr, abifilename)
	}
	// if output is set to stdout, we should not generate the test codes
	generateTests := !isOutStdout

	// Generate the contract precompile
	bindedFiles, err := precompilebind.PrecompileBind(types, string(abi), bins, sigs, pkg, lang, libs, aliases, abifilename, generateTests)
	if err != nil {
		utils.Fatalf("Failed to generate precompile: %v", err)
	}

	// Either flush it out to a file or display on the standard output
	// Skip displaying test codes here.
	if isOutStdout {
		for _, file := range bindedFiles {
			if !file.IsTest {
				fmt.Printf("-----file: %s-----\n", file.FileName)
				fmt.Printf("%s\n", file.Content)
			}
		}
		return nil
	}

	if _, err := os.Stat(outFlagStr); os.IsNotExist(err) {
		os.MkdirAll(outFlagStr, 0o700) // Create your file
	}

	for _, file := range bindedFiles {
		outputPath := filepath.Join(outFlagStr, file.FileName)
		if err := os.WriteFile(outputPath, []byte(file.Content), 0o600); err != nil {
			utils.Fatalf("Failed to write generated file %s: %v", file.FileName, err)
		}
	}

	// Write the ABI to the output folder
	if err := os.WriteFile(abipath, abi, 0o600); err != nil {
		utils.Fatalf("Failed to write ABI: %v", err)
	}

	// Write the README to the output folder
	readmeOut := filepath.Join(outFlagStr, "README.md")
	if err := os.WriteFile(readmeOut, []byte(readme), 0o600); err != nil {
		utils.Fatalf("Failed to write README: %v", err)
	}

	fmt.Println("Precompile files generated successfully at: ", outFlagStr)
	return nil
}

func main() {
	log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stderr, log.LevelInfo, true)))

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
