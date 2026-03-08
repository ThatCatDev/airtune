.PHONY: build build-gui clean test

# Build CLI-only (no CGo, no GTK4 required)
build:
	go build -o airtune.exe .

# Build with GUI (requires MSYS2 + GTK4)
# Ensure C:\msys64\mingw64\bin and C:\msys64\usr\bin are in PATH
build-gui:
	CGO_ENABLED=1 go build -o airtune.exe .

# Run in CLI mode
run:
	go run . --cli

# Run tests
test:
	go test ./internal/...

# Clean build artifacts
clean:
	rm -f airtune.exe
