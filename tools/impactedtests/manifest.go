// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import "strings"

func filterManifest(impacted []string, partitionTests []string) []string {
	partitionSet := make(map[string]struct{}, len(partitionTests))
	for _, label := range partitionTests {
		partitionSet[label] = struct{}{}
	}

	filtered := make([]string, 0, len(impacted))
	for _, label := range impacted {
		if _, ok := partitionSet[label]; ok {
			filtered = append(filtered, label)
		}
	}
	return filtered
}

func formatManifest(labels []string) string {
	if len(labels) == 0 {
		return ""
	}

	var output strings.Builder
	for _, label := range labels {
		output.WriteString(label)
		output.WriteByte('\n')
	}
	return output.String()
}
