//go:build !darwin && !linux

package app

import "os"

func TakeUpdateSessionHandoff() *os.File { return nil }

func (a *App) updateRestart(string) func() (func(), error) { return nil }
