package main

import (
	"os"
	"strings"
	"sync"
)

var desktopWorkspaceCwdMu sync.Mutex

func (a *App) withActiveWorkspace(fn func() (string, error)) (string, error) {
	var out string
	err := a.withActiveWorkspaceDo(func() error {
		var err error
		out, err = fn()
		return err
	})
	return out, err
}

func (a *App) withActiveWorkspaceDo(fn func() error) error {
	root := strings.TrimSpace(a.activeWorkspaceRoot())
	if root == "" {
		root = "."
	}
	desktopWorkspaceCwdMu.Lock()
	defer desktopWorkspaceCwdMu.Unlock()

	previous, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(root); err != nil {
		return err
	}
	defer os.Chdir(previous)
	return fn()
}
