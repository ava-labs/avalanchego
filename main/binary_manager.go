package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

type application struct {
	path    string
	errChan chan error
	cmd     *exec.Cmd
	setup   bool
}

func newApplication() *application {
	return &application{
		path:    "",
		errChan: make(chan error),
		cmd:     nil,
		setup:   false,
	}
}

type BinaryManager struct {
	prevVsApp *application
	currVsApp *application
}

func NewBinaryManager() *BinaryManager {
	return &BinaryManager{
		prevVsApp: newApplication(),
		currVsApp: newApplication(),
	}
}

func (b *BinaryManager) Start() (chan error, chan error) {

	if b.prevVsApp.setup {
		go b.StartApp(b.prevVsApp)
	}

	// give it a few seconds to avoid any unexpected lock-grabbing
	time.Sleep(10 * time.Second)

	if b.currVsApp.setup {
		go b.StartApp(b.currVsApp)
	}
	return b.prevVsApp.errChan, b.currVsApp.errChan
}

func (b *BinaryManager) StartApp(app *application) {
	fmt.Printf("Starting %s\n", app.path)
	cmd := exec.Command(app.path)
	app.cmd = cmd

	cmd.Stdout = os.Stdout

	// start the command after having set up the pipe
	if err := cmd.Start(); err != nil {
		app.errChan <- err
	}

	if err := cmd.Wait(); err != nil {
		app.errChan <- err
	}
}

func (b *BinaryManager) KillAll() {
	if b.prevVsApp.cmd.Process != nil {
		if err := b.prevVsApp.cmd.Process.Kill(); err != nil {
			fmt.Printf("failed to kill process: %v\n", err)
		}
	}

	if b.currVsApp.cmd.Process != nil {
		if err := b.currVsApp.cmd.Process.Kill(); err != nil {
			fmt.Printf("failed to kill process: %v\n", err)
		}
	}
}
