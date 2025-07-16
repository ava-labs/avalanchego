// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package connecthandler

import (
	"context"
	"encoding/json"
	"fmt"

	"connectrpc.com/connect"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/proto/pb/admin/v1/adminv1connect"
	"github.com/ava-labs/avalanchego/utils/logging"

	adminv1 "github.com/ava-labs/avalanchego/proto/pb/admin/v1"
)

var _ adminv1connect.AdminServiceHandler = (*ConnectAdminService)(nil)

type ConnectAdminService struct {
	admin *admin.Admin
}

// NewConnectAdminService returns a ConnectRPC handler for the Admin API
func NewConnectAdminService(admin *admin.Admin) *ConnectAdminService {
	return &ConnectAdminService{admin: admin}
}

// StartCPUProfiler starts a CPU profile writing to the specified file
func (c *ConnectAdminService) StartCPUProfiler(
	_ context.Context,
	_ *connect.Request[adminv1.StartCPUProfilerRequest],
) (*connect.Response[adminv1.StartCPUProfilerResponse], error) {
	if err := c.admin.StartCPUProfiler(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&adminv1.StartCPUProfilerResponse{}), nil
}

// StopCPUProfiler stops the CPU profile
func (c *ConnectAdminService) StopCPUProfiler(
	_ context.Context,
	_ *connect.Request[adminv1.StopCPUProfilerRequest],
) (*connect.Response[adminv1.StopCPUProfilerResponse], error) {
	if err := c.admin.StopCPUProfiler(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&adminv1.StopCPUProfilerResponse{}), nil
}

// MemoryProfile runs a memory profile writing to the specified file
func (c *ConnectAdminService) MemoryProfile(
	_ context.Context,
	_ *connect.Request[adminv1.MemoryProfileRequest],
) (*connect.Response[adminv1.MemoryProfileResponse], error) {
	if err := c.admin.MemoryProfile(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&adminv1.MemoryProfileResponse{}), nil
}

// LockProfile runs a lock profile writing to the specified file
func (c *ConnectAdminService) LockProfile(
	_ context.Context,
	_ *connect.Request[adminv1.LockProfileRequest],
) (*connect.Response[adminv1.LockProfileResponse], error) {
	if err := c.admin.LockProfile(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&adminv1.LockProfileResponse{}), nil
}

// Alias attempts to alias an endpoint to a new name
func (c *ConnectAdminService) Alias(
	_ context.Context,
	request *connect.Request[adminv1.AliasRequest],
) (*connect.Response[adminv1.AliasResponse], error) {
	jsonRequest := &admin.AliasArgs{
		Endpoint: request.Msg.Endpoint,
		Alias:    request.Msg.Alias,
	}

	if err := c.admin.Alias(nil, jsonRequest, &api.EmptyReply{}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&adminv1.AliasResponse{}), nil
}

// AliasChain attempts to alias a chain to a new name
func (c *ConnectAdminService) AliasChain(
	_ context.Context,
	request *connect.Request[adminv1.AliasChainRequest],
) (*connect.Response[adminv1.AliasChainResponse], error) {
	jsonRequest := &admin.AliasChainArgs{
		Chain: request.Msg.Chain,
		Alias: request.Msg.Alias,
	}

	if err := c.admin.AliasChain(nil, jsonRequest, &api.EmptyReply{}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&adminv1.AliasChainResponse{}), nil
}

// ChainAliases returns the aliases of the chain
func (c *ConnectAdminService) ChainAliases(
	_ context.Context,
	request *connect.Request[adminv1.ChainAliasesRequest],
) (*connect.Response[adminv1.ChainAliasesResponse], error) {
	jsonRequest := &admin.GetChainAliasesArgs{
		Chain: request.Msg.Chain,
	}

	var jsonResponse admin.GetChainAliasesReply
	if err := c.admin.GetChainAliases(nil, jsonRequest, &jsonResponse); err != nil {
		return nil, err
	}

	response := &adminv1.ChainAliasesResponse{
		Aliases: jsonResponse.Aliases,
	}

	return connect.NewResponse(response), nil
}

// Stacktrace returns the current global stacktrace
func (c *ConnectAdminService) Stacktrace(
	_ context.Context,
	_ *connect.Request[adminv1.StacktraceRequest],
) (*connect.Response[adminv1.StacktraceResponse], error) {
	if err := c.admin.Stacktrace(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&adminv1.StacktraceResponse{}), nil
}

// SetLoggerLevel sets the log level and/or display level for loggers
func (c *ConnectAdminService) SetLoggerLevel(
	_ context.Context,
	request *connect.Request[adminv1.SetLoggerLevelRequest],
) (*connect.Response[adminv1.SetLoggerLevelResponse], error) {
	jsonRequest := &admin.SetLoggerLevelArgs{
		LoggerName: request.Msg.LoggerName,
	}

	if request.Msg.LogLevel != "" {
		logLevel, err := logging.ToLevel(request.Msg.LogLevel)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid log level: %w", err))
		}
		jsonRequest.LogLevel = &logLevel
	}

	if request.Msg.DisplayLevel != "" {
		displayLevel, err := logging.ToLevel(request.Msg.DisplayLevel)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid display level: %w", err))
		}
		jsonRequest.DisplayLevel = &displayLevel
	}

	var jsonResponse admin.LoggerLevelReply
	if err := c.admin.SetLoggerLevel(nil, jsonRequest, &jsonResponse); err != nil {
		return nil, err
	}

	protoLoggerLevels := make(map[string]*adminv1.LogAndDisplayLevels)

	for name, levels := range jsonResponse.LoggerLevels {
		protoLoggerLevels[name] = &adminv1.LogAndDisplayLevels{
			LogLevel:     levels.LogLevel.String(),
			DisplayLevel: levels.DisplayLevel.String(),
		}
	}

	return connect.NewResponse(&adminv1.SetLoggerLevelResponse{
		LoggerLevels: protoLoggerLevels,
	}), nil
}

// LoggerLevel returns the log level and display level of loggers
func (c *ConnectAdminService) LoggerLevel(
	_ context.Context,
	request *connect.Request[adminv1.LoggerLevelRequest],
) (*connect.Response[adminv1.LoggerLevelResponse], error) {
	jsonRequest := &admin.GetLoggerLevelArgs{
		LoggerName: request.Msg.LoggerName,
	}

	var jsonResponse admin.LoggerLevelReply
	if err := c.admin.GetLoggerLevel(nil, jsonRequest, &jsonResponse); err != nil {
		return nil, err
	}

	protoLoggerLevels := make(map[string]*adminv1.LogAndDisplayLevels)

	for name, levels := range jsonResponse.LoggerLevels {
		protoLoggerLevels[name] = &adminv1.LogAndDisplayLevels{
			LogLevel:     levels.LogLevel.String(),
			DisplayLevel: levels.DisplayLevel.String(),
		}
	}

	return connect.NewResponse(&adminv1.LoggerLevelResponse{
		LoggerLevels: protoLoggerLevels,
	}), nil
}

// Config returns the config that the node was started with
func (c *ConnectAdminService) Config(
	_ context.Context,
	_ *connect.Request[adminv1.ConfigRequest],
) (*connect.Response[adminv1.ConfigResponse], error) {
	var jsonResponse interface{}
	if err := c.admin.GetConfig(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	// Convert the config to JSON
	configJSON, err := json.Marshal(jsonResponse)
	if err != nil {
		return nil, err
	}

	response := &adminv1.ConfigResponse{
		ConfigJson: string(configJSON),
	}

	return connect.NewResponse(response), nil
}

// DB returns the value of a database entry
func (c *ConnectAdminService) DB(
	_ context.Context,
	request *connect.Request[adminv1.DBRequest],
) (*connect.Response[adminv1.DBResponse], error) {
	jsonRequest := &admin.DBGetArgs{
		Key: request.Msg.Key,
	}

	var jsonResponse admin.DBGetReply
	if err := c.admin.DbGet(nil, jsonRequest, &jsonResponse); err != nil {
		return nil, err
	}

	response := &adminv1.DBResponse{
		Value:     jsonResponse.Value,
		ErrorCode: adminv1.ErrorCode(jsonResponse.ErrorCode),
	}

	return connect.NewResponse(response), nil
}
