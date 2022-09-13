// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func GetMetricsValue(url string, metrics ...string) (map[string]float64, error) {
	lines, err := getHTTPLines(url)
	if err != nil {
		return nil, err
	}
	mm := make(map[string]float64, len(metrics))
	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			continue
		}
		found, name := false, ""
		for _, name = range metrics {
			if !strings.HasPrefix(line, name) {
				continue
			}
			found = true
			break
		}
		if !found || name == "" { // no matched metric found
			continue
		}
		ll := strings.Split(line, " ")
		if len(ll) != 2 {
			continue
		}
		fv, err := strconv.ParseFloat(ll[1], 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %q (%w)", ll, err)
		}
		mm[name] = fv
	}
	return mm, nil
}

func getHTTPLines(url string) ([]string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	rd := bufio.NewReader(resp.Body)
	lines := []string{}
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		lines = append(lines, strings.TrimSpace(line))
	}
	resp.Body.Close()
	return lines, nil
}
