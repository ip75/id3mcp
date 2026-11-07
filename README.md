# id3mcp

MCP-server for Crush: edits ID3 tags of MP3 files and sets covers. No Qt, no kid3, no `id3v2` from id3lib.

`id3v2` 0.1.12 (id3lib) writes Cyrillic as UTF-16 inside ID3v2.3. UTF-8 is not supported: UTF-8 is only in the specification for ID3v2.4, but id3lib API doesn't write it. You can't put covers (APIC frame) through this CLI either. So one program instead of a CLI plus separate tool for images.

Default encoding:

- new file and regular ID3v2.3 edit — **UTF-16**. That's how Russian tags open in players, car stereos and old software;
- file that is already ID3v2.4 stays 2.4 and is written as **UTF-8**;
- `force_version: 4` converts a specific batch to UTF-8. This needs to be requested explicitly.

ID3v1 is stripped on write (`ID3MCP_STRIP_V1=0` to keep it). There's no Cyrillic in v1, and a player that prefers v1 shows mojibake over the normal v2.

## What the server doesn't do

Root is set externally, `ID3MCP_ROOT`. Paths above it, including via symlinks, are not opened. No recursion. No more than `ID3MCP_MAX_FILES` per call (default 25, ceiling 100). `confirm=false` writes nothing. Before the first write, a `file.mp3.id3mcp.bak` appears next to it, and this first backup is not overwritten.

Don't put archives there. Copy a batch to the working folder, check diff, then `confirm=true`.

## Building

