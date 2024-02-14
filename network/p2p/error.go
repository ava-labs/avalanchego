package p2p

import "github.com/ava-labs/avalanchego/snow/engine/common"

var (
	ErrUnexpected = &common.AppError{
		Code: -1,
	}
	ErrNotValidator = &common.AppError{
		Code: -2,
	}
	ErrThrottled = &common.AppError{
		Code: -3,
	}
)
