//go:build !cgo

package main

import "log"

func runGUIOrCLI() {
	log.Println("GUI mode requires CGo and GTK4. Falling back to CLI mode.")
	log.Println("Install MSYS2 + mingw-w64-x86_64-gtk4 and build with CGO_ENABLED=1")
	runCLI()
}
