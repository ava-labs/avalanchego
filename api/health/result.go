// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import "time"

// notYetRunResult is the result that is returned when a HealthCheck hasn't been
// run yet.
var notYetRunResult Result

func init() {
	err := "not yet run"
	notYetRunResult = Result{
		Error: &err,
	}
}

type Result struct {
	// Details of the HealthCheck.
	Details interface{} `json:"message,omitempty"`

	// Error is the string representation of the error returned by the failing
	// HealthCheck. The value is nil if the check passed.
	Error *string `json:"error,omitempty"`

	// Timestamp of the last HealthCheck.
	Timestamp time.Time `json:"timestamp,omitempty"`

	// Duration is the amount of time this HealthCheck last took to evaluate.
	Duration time.Duration `json:"duration"`

	// ContiguousFailures the HealthCheck has returned.
	ContiguousFailures int64 `json:"contiguousFailures,omitempty"`

	// TimeOfFirstFailure of the HealthCheck,
	TimeOfFirstFailure *time.Time `json:"timeOfFirstFailure,omitempty"`
}
