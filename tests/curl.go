// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on github.com/etcd-io/etcd/pkg/expect.

package tests

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// Sends a POST request to the endpoint with JSON body "dataRaw",
// and returns an error if "expect" string is not found in the response.
func CURLPost(endpoint string, dataRaw string, expect string) (string, error) {
	cmdArgs := []string{
		"curl",
		"-L", endpoint,
		"-m", "10",
		"-H", "Content-Type: application/json",
		"-X", "POST",
		"-d", dataRaw,
	}

	ex, err := newExpect(cmdArgs[0], cmdArgs[1:]...)
	if err != nil {
		return "", err
	}
	defer ex.Close()

	return ex.Expect(expect)
}

const DEBUG_LINES_TAIL = 40

type expectProcess struct {
	cmd  *exec.Cmd
	fpty *os.File
	wg   sync.WaitGroup

	mu    sync.RWMutex // protects lines and err
	lines []string
	err   error

	stopSignal os.Signal
}

func newExpect(name string, args ...string) (ep *expectProcess, err error) {
	cmd := exec.Command(name, args...)
	ep = &expectProcess{
		cmd:        cmd,
		stopSignal: syscall.SIGKILL,
	}
	ep.cmd.Stderr = ep.cmd.Stdout
	ep.cmd.Stdin = nil

	if ep.fpty, err = pty.Start(ep.cmd); err != nil {
		return nil, err
	}

	ep.wg.Add(1)
	go ep.read()
	return ep, nil
}

func (ep *expectProcess) read() {
	defer ep.wg.Done()
	printDebugLines := os.Getenv("EXPECT_DEBUG") != ""
	r := bufio.NewReader(ep.fpty)
	for {
		l, err := r.ReadString('\n')
		ep.mu.Lock()
		if l != "" {
			if printDebugLines {
				fmt.Printf("%s-%d: %s", ep.cmd.Path, ep.cmd.Process.Pid, l)
			}
			ep.lines = append(ep.lines, l)
		}
		if err != nil {
			ep.err = err
			ep.mu.Unlock()
			break
		}
		ep.mu.Unlock()
	}
}

func (ep *expectProcess) ExpectFunc(f func(string) bool) (string, error) {
	i := 0

	for {
		ep.mu.Lock()
		for i < len(ep.lines) {
			line := ep.lines[i]
			i++
			if f(line) {
				ep.mu.Unlock()
				return line, nil
			}
		}
		if ep.err != nil {
			ep.mu.Unlock()
			break
		}
		ep.mu.Unlock()
		time.Sleep(time.Millisecond * 100)
	}
	ep.mu.Lock()
	lastLinesIndex := len(ep.lines) - DEBUG_LINES_TAIL
	if lastLinesIndex < 0 {
		lastLinesIndex = 0
	}
	lastLines := strings.Join(ep.lines[lastLinesIndex:], "")
	ep.mu.Unlock()
	return "", fmt.Errorf("match not found."+
		" Set EXPECT_DEBUG for more info Err: %v, last lines:\n%s",
		ep.err, lastLines)
}

// Expect returns the first line containing the given string.
func (ep *expectProcess) Expect(s string) (string, error) {
	return ep.ExpectFunc(func(txt string) bool { return strings.Contains(txt, s) })
}

// Stop kills the expect process and waits for it to exit.
func (ep *expectProcess) Stop() error { return ep.close(true) }

// Signal sends a signal to the expect process
func (ep *expectProcess) Signal(sig os.Signal) error {
	return ep.cmd.Process.Signal(sig)
}

func (ep *expectProcess) Close() error { return ep.close(false) }

func (ep *expectProcess) close(kill bool) error {
	if ep.cmd == nil {
		return ep.err
	}
	if kill {
		ep.Signal(ep.stopSignal)
	}

	err := ep.cmd.Wait()
	ep.fpty.Close()
	ep.wg.Wait()

	if err != nil {
		if !kill && strings.Contains(err.Error(), "exit status") {
			// non-zero exit code
			err = nil
		} else if kill && strings.Contains(err.Error(), "signal:") {
			err = nil
		}
	}

	ep.cmd = nil
	return err
}

func (ep *expectProcess) ProcessError() error {
	if strings.Contains(ep.err.Error(), "input/output error") {
		// TODO: The expect library should not return
		// `/dev/ptmx: input/output error` when process just exits.
		return nil
	}
	return ep.err
}

func (ep *expectProcess) Lines() []string {
	ep.mu.RLock()
	defer ep.mu.RUnlock()
	return ep.lines
}
