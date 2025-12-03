// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

//go:build ignore

// go generate script
//
// This script fixes up a go file to enable/disable the correct cgo directives,
// tailored for use in firewood.go to eliminate linker warnings for production builds.
//
// It scans for blocks of cgo directives, using marker lines like this:
// FIREWOOD_CGO_BEGIN_<FIREWOOD_LD_MODE>
// cgo line 1
// ...
// cgo line n
// FIREWOOD_CGO_END_<FIREWOOD_LD_MODE>
//
// FIREWOOD_LD_MODE is an environment variable that decides which blocks are activated.
// The default value for FIREWOOD_LD_MODE is "LOCAL_LIBS" for local development.
// When building production static libraries, FIREWOOD_LD_MODE is set to "STATIC_LIBS"
// in the github actions workflow.
//
// The script enables CGO directives for the target mode and comments out CGO directives
// that do not match.
//
// CGO directives are already comments and CGO does not allow interleaving regular
// comments with CGO directives. To disable, we must double escape the CGO directives
// with:
//
// // #cgo ...
//
// The go file may contain multiple such blocks, but nesting is not allowed.

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
)

const (
	defaultMode = "LOCAL_LIBS"
)

var errGoFileNotSet = errors.New("GOFILE is not set")

func main() {
	mode := getFirewoodLdMode()

	targetFile, err := getTargetFile()
	if err != nil {
		log.Fatalf("Error switching CGO mode to %s:\n%v", mode, err)
	}

	if err := changeCgoDirectivesForFile(mode, targetFile); err != nil {
		log.Fatalf("Error switching CGO mode to %s:\n%v", mode, err)
	}

	fmt.Printf("Successfully switched CGO directives to %s mode\n", mode)
}

// getFirewoodLdMode returns the FIREWOOD_LD_MODE environment variable.
// Defaults to "LOCAL_LIBS".
func getFirewoodLdMode() string {
	mode, ok := os.LookupEnv("FIREWOOD_LD_MODE")
	if !ok {
		mode = "LOCAL_LIBS"
	}
	return mode
}

func getTargetFile() (string, error) {
	targetFile, ok := os.LookupEnv("GOFILE")
	if !ok {
		return "", errGoFileNotSet
	}
	return targetFile, nil
}

func changeCgoDirectivesForFile(targetMode string, targetFile string) error {
	originalFileContent, err := os.ReadFile(targetFile)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", targetFile, err)
	}

	fileLines := strings.Split(string(originalFileContent), "\n")

	// Initial state is "None" which does not process any lines
	currentBlockName := "None"
	for i, line := range fileLines {
		// process state transitions
		// if the line starts with "// FIREWOOD_CGO_BEGIN_", set the state to the text after the prefix
		if newBlockName, ok := strings.CutPrefix(line, "// // FIREWOOD_CGO_BEGIN_"); ok {
			if currentBlockName != "None" {
				return fmt.Errorf("[ERROR] %s:%d: nested CGO blocks not allowed (found %s after %s)", targetFile, i+1, newBlockName, currentBlockName)
			}
			currentBlockName = newBlockName
			continue
		} else if line == fmt.Sprintf("// // FIREWOOD_CGO_END_%s", currentBlockName) {
			currentBlockName = "None"
			continue
		}

		// If we are in a block, process the line
		if currentBlockName != "None" {
			if !isCGODirective(line) {
				return fmt.Errorf("[ERROR] %s:%d: invalid CGO directive in %s section:\n===\n%s\n===\n", targetFile, i+1, currentBlockName, line)
			}
			if currentBlockName == targetMode {
				fileLines[i] = activateCGOLine(fileLines[i])
			} else {
				fileLines[i] = deactivateCGOLine(fileLines[i])
			}
		}
	}

	if currentBlockName != "None" {
		return fmt.Errorf("[ERROR] %s: unterminated CGO block ended in %s", targetFile, currentBlockName)
	}

	// If the contents changed, write it back to the file
	newContents := strings.Join(fileLines, "\n")
	if newContents == string(originalFileContent) {
		fmt.Printf("[INFO] No changes needed to %s\n", targetFile)
		return nil
	}
	return os.WriteFile(targetFile, []byte(newContents), 0644)
}

func isCGODirective(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "// #cgo") || strings.HasPrefix(trimmed, "// // #cgo")
}

func activateCGOLine(line string) string {
	// Convert "// // #cgo" to "// #cgo"
	return strings.Replace(line, "// // #cgo", "// #cgo", 1)
}
func deactivateCGOLine(line string) string {
	// Convert "// #cgo" to "// // #cgo" (but not "// // #cgo" to "// // // #cgo")
	if strings.Contains(line, "// #cgo") && !strings.Contains(line, "// // #cgo") {
		return strings.Replace(line, "// #cgo", "// // #cgo", 1)
	}
	// Already deactivated
	return line
}
