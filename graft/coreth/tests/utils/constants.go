// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import "time"

const (
	// Timeout to boot the AvalancheGo node
	BootAvalancheNodeTimeout = 5 * time.Minute

	// Timeout for the health API to check the AvalancheGo is ready
	HealthCheckTimeout = 5 * time.Second

	DefaultLocalNodeURI = "http://127.0.0.1:9650"
)
