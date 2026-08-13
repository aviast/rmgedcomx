//go:build !windows

package main

func rootsMagicRunning() (bool, error) {
	return false, nil
}
