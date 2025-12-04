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
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package bind generates Ethereum contract Go bindings.
//
// Detailed usage document and tutorial available on the go-ethereum Wiki page:
// https://github.com/ethereum/go-ethereum/wiki/Native-DApps:-Go-bindings-to-Ethereum-contracts

package precompilebind

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ava-labs/subnet-evm/accounts/abi/bind"
	"github.com/ava-labs/subnet-evm/precompile/allowlist"
)

var errNoAnonymousEvent = errors.New("event type must not be anonymous")

const (
	ContractFileName     = "contract.go"
	ConfigFileName       = "config.go"
	ModuleFileName       = "module.go"
	EventFileName        = "event.go"
	ContractTestFileName = "contract_test.go"
	ConfigTestFileName   = "config_test.go"
)

type PrecompileBindFile struct {
	// FileName is the name of the file to be generated.
	FileName string
	// Content is the content of the file to be generated.
	Content string
	// IsTest indicates whether the file is a test file.
	IsTest bool
}

func NewPrecompileBindFile(fileName string, content string, isTest bool) PrecompileBindFile {
	return PrecompileBindFile{
		FileName: fileName,
		Content:  content,
		IsTest:   isTest,
	}
}

// PrecompileBind generates a Go binding for a precompiled contract. It returns a slice of
// PrecompileBindFile structs containing the file name and its contents.
func PrecompileBind(types []string, abiData string, bytecodes []string, fsigs []map[string]string, pkg string, lang bind.Lang, libs map[string]string, aliases map[string]string, abifilename string, generateTests bool) ([]PrecompileBindFile, error) {
	// create hooks
	configHook := createPrecompileHook(abifilename, tmplSourcePrecompileConfigGo)
	contractHook := createPrecompileHook(abifilename, tmplSourcePrecompileContractGo)
	moduleHook := createPrecompileHook(abifilename, tmplSourcePrecompileModuleGo)
	eventHook := createPrecompileHook(abifilename, tmplSourcePrecompileEventGo)
	configTestHook := createPrecompileHook(abifilename, tmplSourcePrecompileConfigTestGo)
	contractTestHook := createPrecompileHook(abifilename, tmplSourcePrecompileContractTestGo)

	if err := verifyABI(abiData); err != nil {
		return nil, err
	}

	abis := []string{abiData}

	configBind, err := bind.BindHelper(types, abis, bytecodes, fsigs, pkg, lang, libs, aliases, configHook)
	if err != nil {
		return nil, fmt.Errorf("failed to generate config binding: %w", err)
	}
	contractBind, err := bind.BindHelper(types, abis, bytecodes, fsigs, pkg, lang, libs, aliases, contractHook)
	if err != nil {
		return nil, fmt.Errorf("failed to generate contract binding: %w", err)
	}
	moduleBind, err := bind.BindHelper(types, abis, bytecodes, fsigs, pkg, lang, libs, aliases, moduleHook)
	if err != nil {
		return nil, fmt.Errorf("failed to generate module binding: %w", err)
	}
	eventBind, err := bind.BindHelper(types, abis, bytecodes, fsigs, pkg, lang, libs, aliases, eventHook)
	if err != nil {
		return nil, fmt.Errorf("failed to generate event binding: %w", err)
	}

	var result []PrecompileBindFile
	result = append(result, NewPrecompileBindFile(ConfigFileName, configBind, false))
	result = append(result, NewPrecompileBindFile(ContractFileName, contractBind, false))
	result = append(result, NewPrecompileBindFile(ModuleFileName, moduleBind, false))
	result = append(result, NewPrecompileBindFile(EventFileName, eventBind, false))

	if generateTests {
		configTestBind, err := bind.BindHelper(types, abis, bytecodes, fsigs, pkg, lang, libs, aliases, configTestHook)
		if err != nil {
			return nil, fmt.Errorf("failed to generate config test binding: %w", err)
		}
		result = append(result, NewPrecompileBindFile(ConfigTestFileName, configTestBind, true))

		contractTestBind, err := bind.BindHelper(types, abis, bytecodes, fsigs, pkg, lang, libs, aliases, contractTestHook)
		if err != nil {
			return nil, fmt.Errorf("failed to generate contract test binding: %w", err)
		}
		result = append(result, NewPrecompileBindFile(ContractTestFileName, contractTestBind, true))
	}

	return result, nil
}

// createPrecompileHook creates a bind hook for precompiled contracts.
func createPrecompileHook(abifilename string, template string) bind.BindHook {
	return func(lang bind.Lang, pkg string, types []string, contracts map[string]*bind.TmplContract, structs map[string]*bind.TmplStruct) (interface{}, string, error) {
		// verify first
		if lang != bind.LangGo {
			return nil, "", errors.New("only GoLang binding for precompiled contracts is supported yet")
		}

		if len(types) != 1 {
			return nil, "", errors.New("cannot generate more than 1 contract")
		}
		funcs := make(map[string]*bind.TmplMethod)

		contract := contracts[types[0]]

		for k, v := range contract.Transacts {
			funcs[k] = v
		}

		for k, v := range contract.Calls {
			funcs[k] = v
		}
		isAllowList := allowListEnabled(funcs)
		if isAllowList {
			// these functions are not needed for binded contract.
			// AllowList struct can provide the same functionality,
			// so we don't need to generate them.
			for key := range allowlist.AllowListABI.Methods {
				delete(funcs, key)
			}
			for events := range allowlist.AllowListABI.Events {
				delete(contract.Events, events)
			}
		}

		precompileContract := &tmplPrecompileContract{
			TmplContract: contract,
			AllowList:    isAllowList,
			Funcs:        funcs,
			ABIFilename:  abifilename,
		}

		data := &tmplPrecompileData{
			Contract: precompileContract,
			Structs:  structs,
			Package:  pkg,
		}
		return data, template, nil
	}
}

func allowListEnabled(funcs map[string]*bind.TmplMethod) bool {
	for key := range allowlist.AllowListABI.Methods {
		if _, ok := funcs[key]; !ok {
			return false
		}
	}
	return true
}

func verifyABI(abiData string) error {
	// check abi first
	evmABI, err := abi.JSON(strings.NewReader(abiData))
	if err != nil {
		return err
	}
	if len(evmABI.Methods) == 0 {
		return errors.New("no ABI methods found")
	}

	for _, event := range evmABI.Events {
		if event.Anonymous {
			return fmt.Errorf("%w: %s", errNoAnonymousEvent, event.Name)
		}
		eventNames := make(map[string]bool)
		for _, arg := range event.Inputs {
			if bind.IsKeyWord(arg.Name) {
				return fmt.Errorf("event input name %s is a keyword", arg.Name)
			}
			name := abi.ToCamelCase(arg.Name)
			if eventNames[name] {
				return fmt.Errorf("normalized event input name is duplicated: %s", name)
			}
			eventNames[name] = true
		}
	}

	for _, method := range evmABI.Methods {
		names := make(map[string]bool)
		for _, input := range method.Inputs {
			if bind.IsKeyWord(input.Name) {
				return fmt.Errorf("input name %s is a keyword", input.Name)
			}
			name := abi.ToCamelCase(input.Name)
			if names[name] {
				return fmt.Errorf("normalized input name is duplicated: %s", name)
			}
			names[name] = true
		}
		names = make(map[string]bool)
		for _, output := range method.Outputs {
			if output.Name == "" {
				return fmt.Errorf("ABI outputs for %s require a name to generate the precompile binding, re-generate the ABI from a Solidity source file with all named outputs", method.Name)
			}
			if bind.IsKeyWord(output.Name) {
				return fmt.Errorf("output name %s is a keyword", output.Name)
			}
			name := abi.ToCamelCase(output.Name)
			if names[name] {
				return fmt.Errorf("normalized output name is duplicated: %s", name)
			}
			names[name] = true
		}
	}

	return nil
}
