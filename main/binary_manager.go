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
	args    []string
}

type BinaryManager struct {
	prevVsApp *application
	currVsApp *application
	rootPath  string
}

func NewBinaryManager(path string) *BinaryManager {
	return &BinaryManager{
		rootPath: path,
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
	fmt.Printf("Starting %s %s \n", app.path, app.args)
	cmd := exec.Command(app.path, app.args...) // #nosec G204
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	app.cmd = cmd
	// start the command after having set up the pipe
	if err := cmd.Start(); err != nil {
		app.errChan <- err
	}

	if err := cmd.Wait(); err != nil {
		app.errChan <- err
	}
}

func (b *BinaryManager) KillAll() {
	if b.prevVsApp.setup && b.prevVsApp.cmd.Process != nil {
		err := b.prevVsApp.cmd.Process.Kill()
		if err != nil && err != os.ErrProcessDone {
			fmt.Printf("failed to kill process: %v\n", err)
		}
	}

	if b.currVsApp.cmd.Process != nil {
		err := b.currVsApp.cmd.Process.Kill()
		if err != nil && err != os.ErrProcessDone {
			fmt.Printf("failed to kill process: %v\n", err)
		}
	}
}
