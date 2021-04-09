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

func (a *application) Start() {
	if !a.setup {
		fmt.Printf("Skipping %s \n", a.path)
		return
	}
	fmt.Printf("Starting %s \n %s \n", a.path, a.args)
	cmd := exec.Command(a.path, a.args...) // #nosec G204
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	a.cmd = cmd
	// start the command after having set up the pipe
	if err := cmd.Start(); err != nil {
		a.errChan <- err
	}

	if err := cmd.Wait(); err != nil {
		a.errChan <- err
	}
}

func (a *application) Kill() error {
	if a.setup && a.cmd.Process != nil {
		//todo change this to interrupt
		err := a.cmd.Process.Kill()
		if err != nil && err != os.ErrProcessDone {
			fmt.Printf("failed to kill process: %v\n", err)
			return err
		}
	}

	return nil
}

type BinaryManager struct {
	PreviousApp *application
	CurrentApp  *application
	rootPath    string
}

func NewBinaryManager(path string) *BinaryManager {
	return &BinaryManager{
		rootPath:    path,
		PreviousApp: &application{errChan: make(chan error)},
		CurrentApp:  &application{errChan: make(chan error)},
	}
}

func (b *BinaryManager) Start() (chan error, chan error) {
	go b.PreviousApp.Start()
	// give it a few seconds to avoid any unexpected lock-grabbing
	time.Sleep(10 * time.Second)

	go b.CurrentApp.Start()
	return b.PreviousApp.errChan, b.CurrentApp.errChan
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
	err := b.PreviousApp.Kill()
	if err != nil {
		fmt.Printf("error killing the previous app %v\n", err)
	}
	err = b.CurrentApp.Kill()
	if err != nil {
		fmt.Printf("error killing the current app %v\n", err)
	}
}
