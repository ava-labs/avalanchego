// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"

	"github.com/ava-labs/avalanchego/app/runner"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/version"
)

// TODO update -- this is boilerplate copied from
// https://github.com/open-telemetry/opentelemetry-go/blob/main/example/fib/main.go
// newExporter returns a console exporter.
func newExporter(w io.Writer) (trace.SpanExporter, error) {
	return stdouttrace.New(
		stdouttrace.WithWriter(w),
		// Use human readable output.
		stdouttrace.WithPrettyPrint(),
		// Do not print timestamps for the demo.
		stdouttrace.WithoutTimestamps(),
	)
}

// TODO update -- this is boilerplate copied from
// https://github.com/open-telemetry/opentelemetry-go/blob/main/example/fib/main.go
// newResource returns a resource describing this application.
func newResource() *resource.Resource {
	r, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("avalanchego"),
		),
	)
	return r
}

func main() {
	fs := config.BuildFlagSet()
	v, err := config.BuildViper(fs, os.Args[1:])

	if errors.Is(err, pflag.ErrHelp) {
		os.Exit(0)
	}

	if err != nil {
		fmt.Printf("couldn't configure flags: %s\n", err)
		os.Exit(1)
	}

	runnerConfig, err := config.GetRunnerConfig(v)
	if err != nil {
		fmt.Printf("couldn't load process config: %s\n", err)
		os.Exit(1)
	}

	if runnerConfig.DisplayVersionAndExit {
		fmt.Print(version.String)
		os.Exit(0)
	}

	nodeConfig, err := config.GetNodeConfig(v, runnerConfig.BuildDir)
	if err != nil {
		fmt.Printf("couldn't load node config: %s\n", err)
		os.Exit(1)
	}

	// Write telemetry data to a file.
	f, err := os.Create("traces.txt")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer f.Close()

	exp, err := newExporter(f)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	tp := trace.NewTracerProvider(
		trace.WithBatcher(exp),
		trace.WithResource(newResource()),
	)

	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			fmt.Println(err)
		}
	}()
	otel.SetTracerProvider(tp)

	runner.Run(runnerConfig, nodeConfig)
}
