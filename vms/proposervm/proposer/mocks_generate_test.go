// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/windower.go -mock_names=Windower=Windower . Windower
