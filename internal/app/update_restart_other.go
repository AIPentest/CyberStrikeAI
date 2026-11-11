//go:build !darwin && !linux

package app

func (a *App) updateRestart(string) func() (func(), error) { return nil }
