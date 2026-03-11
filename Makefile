.PHONY: build build-debug clean test

# Build GUI app (requires MSYS2 + GTK4)
# Ensure C:\msys64\mingw64\bin and C:\msys64\usr\bin are in PATH
build:
	CGO_ENABLED=1 go build -ldflags "-s -w -H windowsgui" -o bundle/airtune.exe .

# Build without hiding console (for debugging)
build-debug:
	CGO_ENABLED=1 go build -o bundle/airtune.exe .

# Run tests
test:
	CGO_ENABLED=1 go test ./internal/...

# Clean build artifacts
clean:
	rm -f bundle/airtune.exe
