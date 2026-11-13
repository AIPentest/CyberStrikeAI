//go:build !darwin && !linux

package app

import "os"

func TakeUpdateSessionHandoff() *os.File { return nil }

func TakeUpdateActivation() string { return "" }

func (a *App) updateRestart(string) func(string) (func(), error) { return nil }
