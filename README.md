# id3mcp

`id3mcp` is a Go MCP (Model Context Protocol) server for reading and editing ID3 tags in MP3 files. It works over stdio and can be used with any MCP-compatible agent or client, not just a specific one.

The server focuses on safe, local tag editing: it exposes MCP tools for listing files, reading tags, setting text fields, adding/replacing front covers, and extracting covers. All file access is restricted to a configured root directory.

## Features

- MCP stdio server (`id3mcp` binary)
- ID3v2 tag reading and writing for `.mp3` files
- Text tag editing: title, artist, album, album artist, genre, year, track, disc, composer, comment
- Cover management: set front cover from `.jpg/.jpeg/.png`, extract existing cover
- Directory listing with optional recursion (`id3_list`, `recursive=true`)
- Preview mode for mutating operations (`confirm=false`)
- Backup before first write (`<file>.mp3.id3mcp.bak`)
- Jail/root protection: paths must stay inside `ID3MCP_ROOT` (including symlink resolution)
- Cyrillic/Unicode handling with ID3v2 version-aware encoding:
  - ID3v2.3 writes UTF-16
  - ID3v2.4 writes UTF-8
  - existing v2.4 files stay v2.4 unless explicitly forced

## Requirements

- Go 1.27+ (as declared in `go.mod`)
- Node.js (for the MCP integration test script)
- `make` (optional, for convenience targets)

## Install

Install the latest tagged release with Go:

```bash
go install github.com/ip75/id3mcp@latest
```

Or install a specific version:

```bash
go install github.com/ip75/id3mcp@v0.1.0
```

The `id3mcp` binary will be placed in `$GOBIN` or `$HOME/go/bin` (Windows: `%USERPROFILE%\go\bin`).

## Build from source

```bash
git clone https://github.com/ip75/id3mcp.git
cd id3mcp
go build -o id3mcp.exe .
```

Cross-build targets from `Makefile`:

```bash
make freebsd
make linux
make windows
make all
```

## Run as MCP server

`id3mcp` communicates over MCP stdio. Set `ID3MCP_ROOT` to the directory containing MP3 files the server is allowed to access.

If installed via `go install`, make sure your Go bin directory is on `PATH`, or use the full path to the installed binary.

Example (PowerShell):

```powershell
$env:ID3MCP_ROOT = "C:\music\work"
id3mcp
# or, from a local build:
.\id3mcp.exe
```

Example (bash):

```bash
export ID3MCP_ROOT=/path/to/music/work
id3mcp
# or, from a local build:
./id3mcp
```

The process expects an MCP client on stdin/stdout. Logs/errors go to stderr.

## MCP client configuration example

Configure your MCP client to launch the binary with `ID3MCP_ROOT` set. The exact config format depends on the client; the important parts are command, args, and environment.

Generic JSON example:

```json
{
  "mcpServers": {
    "id3mcp": {
      "command": "C:\\path\\to\\id3mcp.exe",
      "args": [],
      "env": {
        "ID3MCP_ROOT": "C:\\path\\to\\music\\work"
      }
    }
  }
}
```

Crush (`crushrc`) example:

```bash
mcp add id3mcp \
  --command "C:\\Users\\IP\\go\\bin\\id3mcp.exe" \
  --env ID3MCP_ROOT="C:\\music\\work"
```

If you built from source or placed the binary elsewhere, use that path in `--command`. On Linux/macOS, use the corresponding binary path, e.g. `~/go/bin/id3mcp`.

## Agent skill

The repo includes an agent skill at [`skills/id3mcp/SKILL.md`](skills/id3mcp/SKILL.md). It teaches an agent when and how to use the `id3mcp` MCP tools safely (preview before writes, batch limits, root jail, ID3 version behavior).

Install/copy it into your skills directory, for example:

```bash
# Example: copy into a local skills directory
cp -r skills/id3mcp ~/.agents/skills/id3mcp
```

## Supported MCP tools

| Tool | Description | Mutates files |
|---|---|---|
| `id3_list` | List `.mp3` files in a directory inside `ID3MCP_ROOT`. Set `recursive=true` to include subdirectories. | No |
| `id3_read` | Read tags and cover summary (no image bytes). | No |
| `id3_set` | Edit text tags in a batch. Use `confirm=false` for preview, `confirm=true` to write. | Yes |
| `id3_set_cover` | Set front cover from `.jpg/.jpeg/.png` inside the working root. | Yes |
| `id3_extract_cover` | Extract cover to a file inside the working root. | Yes (writes extracted image file) |

### Notes on behavior

- `id3_set` and `id3_set_cover` require `confirm=false` for a safe preview before writing.
- Empty string clears a text field in `id3_set`.
- `track` and `disc` accept values like `"3"` or `"3/12"`.
- New files/default writes use ID3v2.3 with UTF-16. ID3v2.4 files remain UTF-8. Use `force_version=4` to write UTF-8/ID3v2.4 explicitly.
- ID3v1 is stripped on write by default (`ID3MCP_STRIP_V1=0` keeps it).
- `id3_list` can recurse with `recursive=true`; it returns up to 200 files and sets `truncated=true` if more exist. Apply `id3_set`/`id3_set_cover` in batches (`ID3MCP_MAX_FILES`).

## Configuration

Environment variables:

| Variable | Default | Description |
|---|---:|---|
| `ID3MCP_ROOT` | required | Root directory jail. All file paths must resolve inside it. |
| `ID3MCP_MAX_FILES` | `25` | Max files per call (min 1, max 100). |
| `ID3MCP_MAX_BYTES` | `104857600` (100 MiB) | Max MP3 file size. |
| `ID3MCP_MAX_IMAGE` | `2097152` (2 MiB) | Max cover image size. |
| `ID3MCP_DEFAULT_VERSION` | `3` | Default ID3v2 version for new tags (`3` or `4`). |
| `ID3MCP_STRIP_V1` | `true` | Strip ID3v1 on write. Set `0/false/no/off` to keep. |

## Safety model

- All paths are resolved and checked against `ID3MCP_ROOT`; symlink escapes are rejected.
- Only regular files with expected extensions are accepted.
- Mutating tag tools support preview (`confirm=false`).
- A backup file (`*.id3mcp.bak`) is created before the first write and not overwritten by later writes.
- Do not point `ID3MCP_ROOT` at a large archive. Work on a copied batch.

## Development

Run Go tests:

```bash
go test ./...
```

Run MCP integration tests (builds a test binary and exercises all MCP tools over stdio):

```bash
make test-integration
```

Format/vet:

```bash
gofmt -l .
go vet ./...
```

## Project layout

- `main.go` - CLI/bootstrap, config loading, MCP stdio server startup
- `tools.go` - MCP tool registration and handlers
- `tags.go` - ID3 read/write/cover logic
- `jail.go` - path jail and file safety checks
- `tags_test.go` - Go unit tests
- `test_mcp_integration.mjs` - MCP stdio integration test for all tools
- `Makefile` - build and integration-test recipes

## Limitations / non-goals

- No audio transcoding or audio content modification.
- No archive/library management features; intended for controlled batch edits in a working directory.
- The project is not a general-purpose tagger UI; it is an MCP server for agents/clients.
