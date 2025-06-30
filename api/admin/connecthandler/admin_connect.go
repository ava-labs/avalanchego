// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package connecthandler

import (
	"context"
	"encoding/json"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/utils/logging"

	adminv1 "github.com/ava-labs/avalanchego/proto/pb/admin/v1"
	adminv1connect "github.com/ava-labs/avalanchego/proto/pb/admin/v1/adminv1connect"
)

// NewConnectAdminService returns a ConnectRPC-compatible AdminServiceHandler
// that delegates calls to the existing Admin implementation
func NewConnectAdminService(admin *admin.Admin) *ConnectAdminService {
	return &ConnectAdminService{
		Admin: admin,
	}
}

// ConnectAdminService implements the admin.v1.AdminService using the existing Admin service
type ConnectAdminService struct {
	*admin.Admin
}

// Compile-time check: ensure ConnectAdminService implements AdminServiceHandler
var _ adminv1connect.AdminServiceHandler = (*ConnectAdminService)(nil)

// StartCPUProfiler starts a CPU profile writing to the specified file
func (s *ConnectAdminService) StartCPUProfiler(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[emptypb.Empty], error) {
	if err := s.Admin.StartCPUProfiler(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// StopCPUProfiler stops the CPU profile
func (s *ConnectAdminService) StopCPUProfiler(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[emptypb.Empty], error) {
	if err := s.Admin.StopCPUProfiler(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// MemoryProfile runs a memory profile writing to the specified file
func (s *ConnectAdminService) MemoryProfile(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[emptypb.Empty], error) {
	if err := s.Admin.MemoryProfile(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// LockProfile runs a lock profile writing to the specified file
func (s *ConnectAdminService) LockProfile(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[emptypb.Empty], error) {
	if err := s.Admin.LockProfile(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// Alias attempts to alias an HTTP endpoint to a new name
func (s *ConnectAdminService) Alias(
	_ context.Context,
	req *connect.Request[adminv1.AliasArgs],
) (*connect.Response[emptypb.Empty], error) {
	jsonArgs := &admin.AliasArgs{
		Endpoint: req.Msg.Endpoint,
		Alias:    req.Msg.Alias,
	}

	if err := s.Admin.Alias(nil, jsonArgs, &api.EmptyReply{}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// AliasChain attempts to alias a chain to a new name
func (s *ConnectAdminService) AliasChain(
	_ context.Context,
	req *connect.Request[adminv1.AliasChainArgs],
) (*connect.Response[emptypb.Empty], error) {
	jsonArgs := &admin.AliasChainArgs{
		Chain: req.Msg.Chain,
		Alias: req.Msg.Alias,
	}

	if err := s.Admin.AliasChain(nil, jsonArgs, &api.EmptyReply{}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// GetChainAliases returns the aliases of the chain
func (s *ConnectAdminService) GetChainAliases(
	_ context.Context,
	req *connect.Request[adminv1.GetChainAliasesArgs],
) (*connect.Response[adminv1.GetChainAliasesReply], error) {
	jsonArgs := &admin.GetChainAliasesArgs{
		Chain: req.Msg.Chain,
	}

	var jsonReply admin.GetChainAliasesReply
	if err := s.Admin.GetChainAliases(nil, jsonArgs, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	reply := &adminv1.GetChainAliasesReply{
		Aliases: jsonReply.Aliases,
	}

	return connect.NewResponse(reply), nil
}

// Stacktrace returns the current global stacktrace
func (s *ConnectAdminService) Stacktrace(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[emptypb.Empty], error) {
	if err := s.Admin.Stacktrace(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// SetLoggerLevel sets the log level and/or display level for loggers
func (s *ConnectAdminService) SetLoggerLevel(
	_ context.Context,
	req *connect.Request[adminv1.SetLoggerLevelArgs],
) (*connect.Response[adminv1.SetLoggerLevelReply], error) {
	// Convert the request to the format used by the JSON-RPC API
	jsonArgs := &admin.SetLoggerLevelArgs{
		LoggerName: req.Msg.LoggerName,
	}

	// Handle optional log level parameters
	if req.Msg.LogLevel != "" {
		logLevel, err := logging.ToLevel(req.Msg.LogLevel)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid log level: %w", err))
		}
		jsonArgs.LogLevel = &logLevel
	}

	if req.Msg.DisplayLevel != "" {
		displayLevel, err := logging.ToLevel(req.Msg.DisplayLevel)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid display level: %w", err))
		}
		jsonArgs.DisplayLevel = &displayLevel
	}

	var jsonReply admin.LoggerLevelReply
	if err := s.Admin.SetLoggerLevel(nil, jsonArgs, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(convertSetLoggerLevelReply(jsonReply)), nil
}

// GetLoggerLevel returns the log level and display level of all loggers
func (s *ConnectAdminService) GetLoggerLevel(
	_ context.Context,
	req *connect.Request[adminv1.GetLoggerLevelArgs],
) (*connect.Response[adminv1.GetLoggerLevelReply], error) {
	jsonArgs := &admin.GetLoggerLevelArgs{
		LoggerName: req.Msg.LoggerName,
	}

	var jsonReply admin.LoggerLevelReply
	if err := s.Admin.GetLoggerLevel(nil, jsonArgs, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(convertGetLoggerLevelReply(jsonReply)), nil
}

// GetConfig returns the config that the node was started with
func (s *ConnectAdminService) GetConfig(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[adminv1.GetConfigReply], error) {
	var jsonReply interface{}
	if err := s.Admin.GetConfig(nil, nil, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Convert the config to JSON
	configJSON, err := json.Marshal(jsonReply)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to marshal config to JSON: %w", err))
	}

	reply := &adminv1.GetConfigReply{
		ConfigJson: string(configJSON),
	}
	return connect.NewResponse(reply), nil
}

// LoadVMs loads any new VMs available to the node
func (s *ConnectAdminService) LoadVMs(
	_ context.Context,
	_ *connect.Request[emptypb.Empty],
) (*connect.Response[adminv1.LoadVMsReply], error) {
	var jsonReply admin.LoadVMsReply
	if err := s.Admin.LoadVMs(nil, nil, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Convert the map of ids.ID -> []string to map of string -> VMAliases
	newVMs := make(map[string]*adminv1.VMAliases)
	for vmID, aliases := range jsonReply.NewVMs {
		newVMs[vmID.String()] = &adminv1.VMAliases{
			Aliases: aliases,
		}
	}

	// Convert the map of ids.ID -> string to map of string -> string
	failedVMs := make(map[string]string)
	for vmID, errMsg := range jsonReply.FailedVMs {
		failedVMs[vmID.String()] = errMsg
	}

	reply := &adminv1.LoadVMsReply{
		NewVms:    newVMs,
		FailedVms: failedVMs,
	}

	return connect.NewResponse(reply), nil
}

// DBGet returns the value of a database entry
func (s *ConnectAdminService) DBGet(
	_ context.Context,
	req *connect.Request[adminv1.DBGetArgs],
) (*connect.Response[adminv1.DBGetReply], error) {
	jsonArgs := &admin.DBGetArgs{
		Key: req.Msg.Key,
	}

	var jsonReply admin.DBGetReply
	if err := s.Admin.DBGet(nil, jsonArgs, &jsonReply); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	reply := &adminv1.DBGetReply{
		Value:     jsonReply.Value,
		ErrorCode: adminv1.ErrorCode(jsonReply.ErrorCode),
	}

	return connect.NewResponse(reply), nil
}

// Helper function to convert LoggerLevelReply to protobuf format
func convertSetLoggerLevelReply(jsonReply admin.LoggerLevelReply) *adminv1.SetLoggerLevelReply {
	protoLoggerLevels := make(map[string]*adminv1.LogAndDisplayLevels)

	for name, levels := range jsonReply.LoggerLevels {
		protoLoggerLevels[name] = &adminv1.LogAndDisplayLevels{
			LogLevel:     levels.LogLevel.String(),
			DisplayLevel: levels.DisplayLevel.String(),
		}
	}

	return &adminv1.SetLoggerLevelReply{
		LoggerLevels: protoLoggerLevels,
	}
}

func convertGetLoggerLevelReply(jsonReply admin.LoggerLevelReply) *adminv1.GetLoggerLevelReply {
	protoLoggerLevels := make(map[string]*adminv1.LogAndDisplayLevels)

	for name, levels := range jsonReply.LoggerLevels {
		protoLoggerLevels[name] = &adminv1.LogAndDisplayLevels{
			LogLevel:     levels.LogLevel.String(),
			DisplayLevel: levels.DisplayLevel.String(),
		}
	}

	return &adminv1.GetLoggerLevelReply{
		LoggerLevels: protoLoggerLevels,
	}
}
