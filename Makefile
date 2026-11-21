# Makefile for id3mcp - MCP server for ID3v2 tags
# Builds for FreeBSD, Linux, and Windows

.PHONY: all freebsd linux windows clean test-integration

all: freebsd linux windows

# FreeBSD build
freebsd:
	GOOS=freebsd GOARCH=amd64 go build -o id3mcp-freebsd-amd64 ./...

# Linux build
linux:
	GOOS=linux GOARCH=amd64 go build -o id3mcp-linux-amd64 ./...

# Windows build
windows:
	GOOS=windows GOARCH=amd64 go build -o id3mcp-windows-amd64.exe ./...

# Integration test: runs MCP stdio tests against all supported tools.
test-integration:
	go build -o id3mcp-test.exe .
	node test_mcp_integration.mjs ./id3mcp-test.exe
	rm -f id3mcp-test.exe

# Clean build artifacts
clean:
	rm -f id3mcp-freebsd-amd64 id3mcp-linux-amd64 id3mcp-windows-amd64.exe id3mcp-test.exe
