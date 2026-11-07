package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)



// CLI represents command-line interface options.
type CLI struct {
	Version bool `kong:"name='version',help='Show version and exit'"`
	Help    bool `kong:"name='help',help='Show help and exit'"`
}

// main is the entry point of the application.
func main() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)

	var cli CLI
	kong.Parse(&cli)

	if cli.Version {
		fmt.Fprintf(os.Stderr, "id3mcp — MCP for ID3 tags and covers. Transport stdio, root ID3MCP_ROOT.\n")
		os.Exit(0)
	}

	if cli.Help {
		fmt.Fprintf(os.Stderr, "id3mcp speaks MCP over stdin/stdout. Set ID3MCP_ROOT and configure server in Crush.\n")
		os.Exit(0)
	}

	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintf(os.Stderr, "id3mcp speaks MCP over stdin/stdout. Set ID3MCP_ROOT and configure server in Crush.\n")
		os.Exit(2)
	}

	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	a := &app{cfg: cfg}
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "id3mcp",
		Version: "0.1.0",
	}, &mcp.ServerOptions{Instructions: a.instructions()})
	a.register(server)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

// loadConfig loads application configuration from environment variables.
func loadConfig() (Config, error) {
	root := strings.TrimSpace(os.Getenv("ID3MCP_ROOT"))
	if root == "" {
		return Config{}, fmt.Errorf("ID3MCP_ROOT not set: specify working directory, not archive")
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Config{}, fmt.Errorf("ID3MCP_ROOT: %w", err)
	}
	st, err := os.Stat(real)
	if err != nil {
		return Config{}, err
	}
	if !st.IsDir() {
		return Config{}, fmt.Errorf("ID3MCP_ROOT is not a directory: %s", real)
	}
	ver := byte(3)
	if v := os.Getenv("ID3MCP_DEFAULT_VERSION"); v == "4" {
		ver = 4
	}
	return Config{
		Root:           real,
		MaxFiles:       envInt("ID3MCP_MAX_FILES", 25, 1, 100),
		MaxBytes:       int64(envInt("ID3MCP_MAX_BYTES", 100<<20, 1<<20, 1<<30)),
		MaxImage:       int64(envInt("ID3MCP_MAX_IMAGE", 2<<20, 16<<10, 8<<20)),
		DefaultVersion: ver,
		StripV1:        envBool("ID3MCP_STRIP_V1", true),
	}, nil
}

// envInt gets an integer value from environment variable with defaults and bounds.
func envInt(key string, def, min, max int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

// envBool gets a boolean value from environment variable with default.
func envBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "":
		return def
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
