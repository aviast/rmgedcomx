//go:build windows

package main

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

func rootsMagicRunning() (bool, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(snapshot)

	var process windows.ProcessEntry32
	process.Size = uint32(unsafe.Sizeof(process))
	if err := windows.Process32First(snapshot, &process); err != nil {
		return false, err
	}
	for {
		if strings.EqualFold(windows.UTF16ToString(process.ExeFile[:]), "RootsMagic.exe") {
			return true, nil
		}
		if err := windows.Process32Next(snapshot, &process); err != nil {
			if err == windows.ERROR_NO_MORE_FILES {
				return false, nil
			}
			return false, err
		}
	}
}
