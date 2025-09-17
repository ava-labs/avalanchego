// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"encoding/json"
	"os"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/perms"
)

// Defines a mapping of flag keys to values intended to be supplied to
// an invocation of an AvalancheGo node.
type FlagsMap map[string]string

// Utility function simplifying construction of a FlagsMap from a file.
func ReadFlagsMap(path string, description string) (FlagsMap, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, stacktrace.Errorf("failed to read %s: %w", description, err)
	}
	flagsMap := FlagsMap{}
	if err := json.Unmarshal(bytes, &flagsMap); err != nil {
		return nil, stacktrace.Errorf("failed to unmarshal %s: %w", description, err)
	}
	return flagsMap, nil
}

// SetDefault ensures the effectiveness of a flag override by only
// setting a value supplied whose key is not already explicitly set.
func (f FlagsMap) SetDefault(key string, value string) {
	if _, ok := f[key]; !ok {
		f[key] = value
	}
}

// SetDefaults ensures the effectiveness of flag overrides by only
// setting values supplied in the defaults map that are not already
// explicitly set.
func (f FlagsMap) SetDefaults(defaults FlagsMap) {
	for key, value := range defaults {
		f.SetDefault(key, value)
	}
}

// Write simplifies writing a FlagsMap to the provided path. The
// description is used in error messages.
func (f FlagsMap) Write(path string, description string) error {
	bytes, err := DefaultJSONMarshal(f)
	if err != nil {
		return stacktrace.Errorf("failed to marshal %s: %w", description, err)
	}
	if err := os.WriteFile(path, bytes, perms.ReadWrite); err != nil {
		return stacktrace.Errorf("failed to write %s: %w", description, err)
	}
	return nil
}
