package main

import (
	"fmt"
	"os"
	"os/signal"
	"time"

	"airtune/internal/audio"
)

func main() {
	fmt.Println("hooktest: starting...")
	fmt.Printf("hooktest: exe=%s\n", os.Args[0])
	fmt.Printf("hooktest: wd=")
	if wd, err := os.Getwd(); err == nil {
		fmt.Println(wd)
	}

	hook := audio.NewAVSyncHook()

	latencyHNS := int64(20000000) // 2 seconds
	fmt.Printf("hooktest: enabling hook with %dms latency...\n", latencyHNS/10000)

	err := hook.Enable(latencyHNS)
	if err != nil {
		fmt.Printf("hooktest: Enable FAILED: %v\n", err)
		fmt.Println("Press Enter to exit...")
		fmt.Scanln()
		return
	}
	fmt.Println("hooktest: hook enabled!")
	fmt.Println("hooktest: open a NEW Chrome window and play a video.")
	fmt.Println("hooktest: check %TEMP%\\airtune_sync.log for debug output.")
	fmt.Println("hooktest: press Ctrl+C to disable and exit.")

	// Keep running with a message pump (needed for SetWindowsHookEx to dispatch)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sig:
			fmt.Println("\nhooktest: disabling hook...")
			hook.Disable()
			fmt.Println("hooktest: done.")
			return
		case <-ticker.C:
			fmt.Println("hooktest: still running... (hook active)")
		}
	}
}
