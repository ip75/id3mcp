---
name: id3mcp
description: Use when an agent needs to read or edit MP3 ID3 tags and covers through the local id3mcp MCP server, including listing MP3s, previewing changes, editing text fields, setting or extracting front covers, or configuring the id3mcp MCP server in a client.
---

# id3mcp

Use the `id3mcp` MCP server for local MP3 tag operations inside a configured `ID3MCP_ROOT`. The server is Go, stdio-based, and mutates files only through its MCP tools.

## Setup

- Install: `go install github.com/ip75/id3mcp@latest` or build from source with `go build -o id3mcp.exe .`
- Require `ID3MCP_ROOT` pointing at a working directory containing MP3 files. Do not point it at a large archive.
- MCP client config shape:

```json
{
  "mcpServers": {
    "id3mcp": {
      "command": "C:\\path\\to\\id3mcp.exe",
      "args": [],
      "env": { "ID3MCP_ROOT": "C:\\path\\to\\music\\work" }
    }
  }
}
```

- Crush `crushrc`:

```bash
mcp add id3mcp --command "C:\\path\\to\\id3mcp.exe" --env ID3MCP_ROOT "C:\\music\\work"
```

## Workflow

1. Start with `id3_list` or `id3_read`. Use `id3_list` with `recursive=true` only when subdirectories are needed; it returns up to 200 files and `truncated=true` when capped.
2. Before any mutation, call `id3_set` or `id3_set_cover` with `confirm=false` and show the diff/preview to the user.
3. Call the same mutating tool with `confirm=true` only after the user approves the preview.
4. Respect limits: batch size `ID3MCP_MAX_FILES` (default 25), MP3 size `ID3MCP_MAX_BYTES`, image size `ID3MCP_MAX_IMAGE`. Split large jobs into batches.

## Tools

| Tool | Use | Mutates |
|---|---|---|
| `id3_list` | List `.mp3` files in a directory inside root. `recursive=true` includes subdirectories. | No |
| `id3_read` | Read tags and cover summary; no image bytes. | No |
| `id3_set` | Edit text tags. `confirm=false` previews; `confirm=true` writes. Empty string clears field. `track`/`disc`: `"3"` or `"3/12"`. | Yes |
| `id3_set_cover` | Set front cover from `.jpg/.jpeg/.png` inside root. `confirm=false` previews. | Yes |
| `id3_extract_cover` | Extract cover to file inside root. | Yes (image file) |

## Behavior and Safety

- Paths must resolve inside `ID3MCP_ROOT`; symlink escapes and unexpected file types are rejected.
- New/default tags write ID3v2.3 UTF-16. Existing v2.4 stays UTF-8. Use `force_version=4` only when explicitly needed.
- ID3v1 is stripped on write by default; `ID3MCP_STRIP_V1=0` keeps it.
- Mutating writes create `<file>.mp3.id3mcp.bak` before first write and do not overwrite that backup on later writes.
- Do not guess missing fields, do not write covers as text, and do not claim a write succeeded without a confirmed tool result.