// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/targeter.go -mock_names=Targeter=Targeter . Targeter
//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/tracker.go -mock_names=Tracker=Tracker . Tracker
