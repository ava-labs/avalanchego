// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package runner

type Config struct {
	// If true, displays version and exits during startup
	DisplayVersionAndExit bool

	// If true, run AvalancheGo as a plugin
	PluginMode bool
}
