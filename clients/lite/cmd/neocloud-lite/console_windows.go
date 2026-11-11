package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// attachConsole lets the installed app, a GUI build (-H=windowsgui) with
// no console of its own, print the CLI commands' output in the terminal
// that started it. A console build, or output redirected to a file or a
// pipe, keeps what it has.
func attachConsole() {
	// Checked before attaching, which may fill in the process's handles
	// but not the *os.File values Go already made from them.
	noOut := missing(windows.STD_OUTPUT_HANDLE)
	noErr := missing(windows.STD_ERROR_HANDLE)
	noIn := missing(windows.STD_INPUT_HANDLE)
	const attachParentProcess = ^uintptr(0) // ATTACH_PARENT_PROCESS, (DWORD)-1
	attach := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	if ok, _, _ := attach.Call(attachParentProcess); ok == 0 {
		return // already has a console, or no parent console to join
	}
	if noOut {
		if out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stdout = out
		}
	}
	if noErr {
		if out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
			os.Stderr = out
		}
	}
	if noIn {
		if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
			os.Stdin = in
		}
	}
}

func missing(std uint32) bool {
	handle, err := windows.GetStdHandle(std)
	return err != nil || handle == 0 || handle == windows.InvalidHandle
}
