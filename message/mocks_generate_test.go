// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

//go:generate go tool mockgen -package=${GOPACKAGE}mock -destination=${GOPACKAGE}mock/outbound_message_builder.go -mock_names=OutboundMsgBuilder=OutboundMsgBuilder . OutboundMsgBuilder
