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

var _ adminv1connect.AdminServiceHandler = (*connectAdminService)(nil)

type connectAdminService struct {
	admin *admin.Admin
}

// NewConnectAdminService returns a ConnectRPC handler for the Admin API
func NewConnectAdminService(admin *admin.Admin) adminv1connect.AdminServiceHandler {
	return &connectAdminService{
		admin: admin,
	}
}

// StartCPUProfiler starts a CPU profile writing to the specified file
func (c *connectAdminService) StartCPUProfiler(
	_ context.Context,
	_ *connect.Request[adminv1.StartCPUProfilerRequest],
) (*connect.Response[adminv1.StartCPUProfilerResponse], error) {
	if err := c.admin.StartCPUProfiler(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&adminv1.StartCPUProfilerResponse{}), nil
}

// StopCPUProfiler stops the CPU profile
func (c *connectAdminService) StopCPUProfiler(
	_ context.Context,
	_ *connect.Request[adminv1.StopCPUProfilerRequest],
) (*connect.Response[adminv1.StopCPUProfilerResponse], error) {
	if err := c.admin.StopCPUProfiler(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&adminv1.StopCPUProfilerResponse{}), nil
}

// MemoryProfile runs a memory profile writing to the specified file
func (c *connectAdminService) MemoryProfile(
	_ context.Context,
	_ *connect.Request[adminv1.MemoryProfileRequest],
) (*connect.Response[adminv1.MemoryProfileResponse], error) {
	if err := c.admin.MemoryProfile(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&adminv1.MemoryProfileResponse{}), nil
}

// LockProfile runs a lock profile writing to the specified file
func (c *connectAdminService) LockProfile(
	_ context.Context,
	_ *connect.Request[adminv1.LockProfileRequest],
) (*connect.Response[adminv1.LockProfileResponse], error) {
	if err := c.admin.LockProfile(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&adminv1.LockProfileResponse{}), nil
}

// Alias attempts to alias an HTTP endpoint to a new name
func (c *connectAdminService) Alias(
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
func (c *connectAdminService) AliasChain(
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

// GetChainAliases returns the aliases of the chain
func (c *connectAdminService) GetChainAliases(
	_ context.Context,
	request *connect.Request[adminv1.GetChainAliasesRequest],
) (*connect.Response[adminv1.GetChainAliasesResponse], error) {
	jsonRequest := &admin.GetChainAliasesArgs{
		Chain: request.Msg.Chain,
	}

	var jsonResponse admin.GetChainAliasesReply
	if err := c.admin.GetChainAliases(nil, jsonRequest, &jsonResponse); err != nil {
		return nil, err
	}

	Response := &adminv1.GetChainAliasesResponse{
		Aliases: jsonResponse.Aliases,
	}

	return connect.NewResponse(Response), nil
}

// Stacktrace returns the current global stacktrace
func (c *connectAdminService) Stacktrace(
	_ context.Context,
	_ *connect.Request[adminv1.StacktraceRequest],
) (*connect.Response[adminv1.StacktraceResponse], error) {
	if err := c.admin.Stacktrace(nil, nil, &api.EmptyReply{}); err != nil {
		return nil, err
	}

	return connect.NewResponse(&adminv1.StacktraceResponse{}), nil
}

// SetLoggerLevel sets the log level and/or display level for loggers
func (c *connectAdminService) SetLoggerLevel(
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

// GetLoggerLevel returns the log level and display level of all loggers
func (c *connectAdminService) GetLoggerLevel(
	_ context.Context,
	request *connect.Request[adminv1.GetLoggerLevelRequest],
) (*connect.Response[adminv1.GetLoggerLevelResponse], error) {
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

	return connect.NewResponse(&adminv1.GetLoggerLevelResponse{
		LoggerLevels: protoLoggerLevels,
	}), nil
}

// GetConfig returns the config that the node was started with
func (c *connectAdminService) GetConfig(
	_ context.Context,
	_ *connect.Request[adminv1.GetConfigRequest],
) (*connect.Response[adminv1.GetConfigResponse], error) {
	var jsonResponse interface{}
	if err := c.admin.GetConfig(nil, nil, &jsonResponse); err != nil {
		return nil, err
	}

	// Convert the config to JSON
	configJSON, err := json.Marshal(jsonResponse)
	if err != nil {
		return nil, err
	}

	Response := &adminv1.GetConfigResponse{
		ConfigJson: string(configJSON),
	}

	return connect.NewResponse(Response), nil
}

// DBGet returns the value of a database entry
func (c *connectAdminService) DBGet(
	_ context.Context,
	request *connect.Request[adminv1.DBGetRequest],
) (*connect.Response[adminv1.DBGetResponse], error) {
	jsonRequest := &admin.DBGetArgs{
		Key: request.Msg.Key,
	}

	var jsonResponse admin.DBGetReply
	if err := c.admin.DbGet(nil, jsonRequest, &jsonResponse); err != nil {
		return nil, err
	}

	Response := &adminv1.DBGetResponse{
		Value:     jsonResponse.Value,
		ErrorCode: adminv1.ErrorCode(jsonResponse.ErrorCode),
	}

	return connect.NewResponse(Response), nil
}
