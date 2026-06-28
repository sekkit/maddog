//go:build darwin

package main

/*
#cgo darwin LDFLAGS: -framework Cocoa
void installMaddogSystemQuitHook(void);
*/
import "C"

import "sync"

var installSystemQuitHookOnce sync.Once

func installSystemQuitHook() {
	installSystemQuitHookOnce.Do(func() {
		C.installMaddogSystemQuitHook()
	})
}

//export MaddogMarkSystemQuit
func MaddogMarkSystemQuit() {
	markSystemQuitRequested()
}
