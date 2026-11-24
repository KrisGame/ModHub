//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

type TrayCallbacks struct {
	OnOpen    func()
	OnRestart func()
	OnExit    func()
}

func RunSystemTray(_ string, cbs TrayCallbacks) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	<-ch
	if cbs.OnExit != nil {
		cbs.OnExit()
	}
}
