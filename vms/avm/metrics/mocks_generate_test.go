// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/metrics.go -mock_names=Metrics=Metrics . Metrics
