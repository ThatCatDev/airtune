package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"airtune/internal/audio"
	"airtune/internal/codec"
	"airtune/internal/discovery"
	"airtune/internal/raop"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// If GUI is available (CGo build), try GUI mode unless --cli is passed.
	// Otherwise fall back to CLI mode.
	for _, arg := range os.Args[1:] {
		if arg == "--cli" || arg == "-cli" {
			runCLI()
			return
		}
	}

	// Try GUI mode — runGUI is defined in main_gui.go (cgo) or main_nogui.go
	runGUIOrCLI()
}

// runCLI runs in interactive CLI mode — discovers devices and lets you choose.
func runCLI() {
	fmt.Println("AirTune - CLI Mode")
	fmt.Println("==================")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	reader := bufio.NewReader(os.Stdin)

	// Discovery
	fmt.Println("Searching for AirPlay devices...")
	var devices []discovery.AirPlayDevice
	deviceCh := make(chan struct{}, 1)

	browser := discovery.NewBrowser(func(found []discovery.AirPlayDevice) {
		devices = found
		select {
		case deviceCh <- struct{}{}:
		default:
		}
	})

	go browser.Start(ctx)

	// Wait for initial discovery (up to 5s)
	select {
	case <-deviceCh:
		// Got at least one, wait a bit more for others
		time.Sleep(2 * time.Second)
	case <-time.After(5 * time.Second):
	}

	if len(devices) == 0 {
		fmt.Println("No AirPlay devices found.")
		return
	}

	// Interactive loop
	var session *raop.Session
	var pipeline *audio.Pipeline
	var encoder codec.Encoder
	var streamCancel context.CancelFunc
	var connectedName string

	for {
		fmt.Println()
		if connectedName != "" {
			fmt.Printf("Currently streaming to: %s\n", connectedName)
		}
		fmt.Println("Discovered devices:")
		for i, dev := range devices {
			fmt.Printf("  [%d] %s (%s:%d) model=%s\n",
				i+1, dev.Name, dev.Host, dev.Port, dev.Model)
		}
		fmt.Println()
		if connectedName != "" {
			fmt.Println("Commands: [number] connect to device, [d] disconnect, [q] quit")
		} else {
			fmt.Println("Commands: [number] connect to device, [r] refresh, [q] quit")
		}
		fmt.Print("> ")

		input, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		input = strings.TrimSpace(input)

		switch strings.ToLower(input) {
		case "q", "quit", "exit":
			if streamCancel != nil {
				streamCancel()
			}
			if session != nil {
				session.Close()
			}
			if pipeline != nil {
				pipeline.Stop()
			}
			fmt.Println("Goodbye!")
			return

		case "d", "disconnect":
			if session == nil {
				fmt.Println("Not connected to any device.")
				continue
			}
			if streamCancel != nil {
				streamCancel()
			}
			session.Close()
			session = nil
			if pipeline != nil {
				pipeline.Stop()
				pipeline = nil
			}
			fmt.Printf("Disconnected from %s\n", connectedName)
			connectedName = ""
			continue

		case "r", "refresh":
			fmt.Println("Refreshing device list...")
			time.Sleep(2 * time.Second)
			continue

		default:
			choice, err := strconv.Atoi(input)
			if err != nil || choice < 1 || choice > len(devices) {
				fmt.Printf("Invalid input: %s\n", input)
				continue
			}

			target := devices[choice-1]

			// Disconnect existing if any
			if session != nil {
				if streamCancel != nil {
					streamCancel()
				}
				session.Close()
				session = nil
				if pipeline != nil {
					pipeline.Stop()
					pipeline = nil
				}
			}

			fmt.Printf("Connecting to %s...\n", target.Name)

			encoder = codec.NewPCMEncoder()
			session = raop.NewSession(target.Host, target.Port, encoder.CodecName(), encoder.FmtpLine(), target.SupportsEncryption())
			session.OnStateChange(func(state raop.SessionState) {
				log.Printf("Session state: %s", state)
			})

			if err := session.Connect(ctx); err != nil {
				fmt.Printf("Failed to connect: %v\n", err)
				session = nil
				continue
			}

			pipeline = audio.NewPipeline(encoder, audio.NewWASAPILoopbackCapturer(""))
			if err := pipeline.Start(ctx); err != nil {
				fmt.Printf("Failed to start audio pipeline: %v\n", err)
				session.Close()
				session = nil
				continue
			}

			audioCh := pipeline.Subscribe(target.ID, audio.ChannelBoth)

			streamCtx, sc := context.WithCancel(ctx)
			streamCancel = sc

			go func() {
				session.Start(audioCh)
				// If Start returns, streaming ended
				select {
				case <-streamCtx.Done():
				default:
					fmt.Println("\nStreaming ended.")
				}
			}()

			connectedName = target.Name
			fmt.Printf("Streaming to %s! Use 'd' to disconnect.\n", target.Name)
		}
	}
}
