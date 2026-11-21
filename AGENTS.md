# AGENTS.md

Go project: an MCP server (stdio) for reading and editing ID3v2 tags in MP3 files.
Scope: text fields, covers (APIC), localized text (different languages and encodings), and different tag versions.

Code, identifiers, comments, and commits are in English. This file is in English; standard terms remain in English.

---

## Permissions and Tools

- You may freely edit, create, and delete files inside the current project directory (unless it conflicts with safety rules or touches secrets such as `.env`).
- You may use all connected MCP tools, terminal, search, and other available tools without unnecessary confirmation when needed to complete the task.
- Before destructive actions (deleting large amounts of data, resetting git changes, overwriting generated artifacts), be careful and check the task context.

## 0. Main Rule: The Standard Is the Only Source of Truth

Do not invent behavior.

- If something is not in the specification, do not implement it "as others usually do". Stop, record the question in `docs/DECISIONS.md`, and ask the owner.
- Every flag, field, and constraint in code must be accompanied by a comment with a section reference, e.g. `// ID3v2.4.0 frames §4.14 (APIC)`.
- Behavior of other programs (iTunes, TagLib, mutagen, Windows Explorer) is not an argument. They may be used only as external oracles in tests (§9).
- Do not type code tables (ISO 639-2, etc.) from memory. Take them from the primary source and embed via `go:embed`.
- Do not add frames, flags, or "convenient" conventions that are not in the specifications.
- Do not write dependency API versions from memory: check `go doc` / pkg.go.dev for the version from `go.mod`.

---

## 1. Specifications

Put unchanged copies in `docs/spec/` (the document license allows copying without changes with a reference to the original). Before working on a frame, open its section and follow the text.

| Document | Address |
|---|---|
| ID3v2.3.0 (informal standard, 3 Feb 1999) | https://id3.org/id3v2.3.0 |
| ID3v2.4.0 Main Structure (1 Nov 2000) | https://id3.org/id3v2.4.0-structure |
| ID3v2.4.0 Native Frames (1 Nov 2000) | https://id3.org/id3v2.4.0-frames |
| ID3v2.4.0 Changes (differences 2.3 → 2.4) | https://id3.org/id3v2.4.0-changes |
| Chapter Frame Addendum 1.0 (CHAP, CTOC; compatible with 2.3 and 2.4) | https://id3.org/Developer%20Information |
| Accessibility Addendum 1.0 (ATXT; compatible with 2.3 and 2.4) | https://id3.org/Developer%20Information |
| ID3v2.2.0 — obsolete, replaced by 2.3.0 | https://id3.org/id3v2-00 |

**What counts as "the newest standard".** As of 2026-11-04 this is ID3v2.4.0 (1 November 2000, "informal standard"). There is no newer main version on id3.org. After 2.4.0 only addenda were released (chapters, accessibility). Check every six months: https://id3.org/Developer%20Information.

---

## 2. Scope

Required:
- Read tags 2.3 and 2.4; write 2.3 and 2.4.
- Text frames (T***, TXXX), URL frames (W***, WXXX), COMM, USLT, APIC.
- Any Unicode characters in text via correct encodings (§5).
- Multiple languages via frames with a Language field (§6).
- Covers: add, replace, extract, delete (§7).
- Preserve all unknown frames when editing (§8).

Later, only after owner decision: SYLT, POPM, PCNT, UFID, PRIV, GEOB, CHAP/CTOC, ID3v2.2, addenda.

Out of scope: changing audio data; ID3v1 (read and preserve as-is, do not change without explicit request); ID3v2 tags inside non-MP3 containers.

---

## 3. Project Structure (proposal; if the repo already has structure, follow it)

```
cmd/server/main.go        // only MCP stdio startup
internal/id3v2/           // tag codec, knows nothing about MCP
    header.go             // header, extended header, footer, synchsafe, unsync
    frame.go              // frame header, flags (by version!)
    text.go               // encodings, terminators, multi-string
    frames_text.go        // T***, TXXX, W***, WXXX
    frames_comm.go        // COMM, USLT
    frames_apic.go        // APIC
    convert.go            // 2.3 <-> 2.4
    validate.go           // uniqueness, value formats
    file.go               // tag lookup in file, tag replacement, atomic write
internal/mcpserver/       // tool descriptions, input validation, id3v2 calls
docs/spec/                // specification copies
docs/DECISIONS.md         // decision/question log for owner
testdata/                 // small reference files
```

Rules:
- `internal/id3v2` must not import MCP and must not write to stdout.
- MCP over stdio: **stdout is reserved for JSON-RPC**. Logs go only to stderr (`log` with `os.Stderr`). No `fmt.Println`.
- SDK: official `github.com/modelcontextprotocol/go-sdk`, stdio transport. Dependencies: SDK and standard library. `golang.org/x/text` only for optional Windows-1251 reading (§5.5). New dependencies only with owner permission.

---

## 4. Version Cheat Sheet (verified against specs)

2.3 and 2.4 parsers/writers are **separate**: flag bits and sizes are encoded differently. Do not reuse flag parsing between versions.

| Aspect | 2.3.0 | 2.4.0 |
|---|---|---|
| Tag header | `"ID3" $03 00`, flags `%abc00000`: a unsync (bit 7), b ext header (bit 6), c experimental (bit 5) | `"ID3" $04 00`, flags `%abcd0000`: same + d footer (bit 4) |
| Tag size | 4 bytes synchsafe (28 bits), excluding 10-byte header, including ext header and padding, after unsync | 4 bytes synchsafe; = ext header + padding + frames after unsync; total−10, or total−20 with footer |
| Extended header | size 4 bytes normal integer (6 or 10), flags 2 bytes (`%x0000000 00000000`, x = CRC), padding size 4 bytes, CRC-32 4 bytes | size synchsafe, `$01` + 1 byte flags `%0bcd0000` (b update, c CRC, d restrictions), each flag has data length byte |
| Footer | none | `"3DI"` + header copy; **required** for a tag at end of file; padding with footer is forbidden |
| Unsynchronisation | whole tag (including ext header), flag in tag header | frame level (flag `n`); tag header flag set only if all frames are unsynced |
| Frame size | 4 bytes big-endian, normal integer, excluding 10-byte header | synchsafe 32 bits, excluding header, size after compression/encryption/unsync |
| Frame flags | `%abc00000 %ijk00000`: a tag-alter, b file-alter, c read-only; i compression, j encryption, k grouping | `%0abc0000 %0h00kmnp`: a, b, c same but different position; h grouping, k compression, m encryption, n unsync, p data length indicator |
| Extra fields after frame header | in flag order: 4 bytes decompressed size (i), 1 byte encryption method (j), 1 byte group (k) | in flag order (§4.1.2); compression requires Data Length Indicator (synchsafe) |
| Encodings | `$00` ISO-8859-1; `$01` Unicode UCS-2 with BOM | `$00` ISO-8859-1; `$01` UTF-16 with BOM; `$02` UTF-16BE without BOM; `$03` UTF-8 |
| Multiple values in T-frame | `/` separator for TCOM, TEXT, TOLY, TOPE, TPE1 | string list with encoding terminator separator |
| Date/time | TYER (`YYYY`), TDAT (`DDMM`), TIME (`HHMM`), TRDA, TORY | TDRC, TDOR, TDRL, TDEN, TDTG — ISO 8601 subset, always UTC |
| Involved people list | IPLS | TMCL (instrument ↔ musician), TIPL (role ↔ name) |
| Volume / EQ | RVAD, EQUA | RVA2, EQU2 |
| TCON | `(21)Eurodisco`, `(RX)`, `(CR)`, escaping `((` | numeric string and/or free text: `"21" $00 "Eurodisco"`; words `RX`, `CR` |
| USER | one per tag | one per language |
| SYLT content type | `$00`–`$06` | `$00`–`$08` |
| APIC description | no longer than 64 characters | no explicit limit in text |
| Padding | zeros after last frame | same, but forbidden between frames and when footer is present |

Obsolete in 2.4 (replaced): EQUA→EQU2, IPLS→TMCL+TIPL, RVAD→RVA2, TDAT/TIME/TRDA/TYER→TDRC, TORY→TDOR, TSIZ — removed without replacement.
New in 2.4: ASPI, EQU2, RVA2, SEEK, SIGN, TDEN, TDOR, TDRC, TDRL, TDTG, TIPL, TMCL, TMOO, TPRO, TSOA, TSOP, TSOT, TSST.

Test vectors directly from specifications:
- 2.3 §3.1: tag size 257 bytes is encoded `$00 00 02 01`.
- 2.4 §6.2: 255 as 16-bit synchsafe = 383 = `%00000001 01111111`.
- Unsync: `$FF %111xxxxx` → `$FF $00 %111xxxxx`; `$FF $00` → `$FF $00 $00`.

Tag detection: `$49 44 33 yy yy xx zz zz zz zz`, where `yy < $FF`, `zz < $80`. Version and revision are never `$FF`. If major version is greater than supported, ignore the entire tag; do not attempt to parse it.

Frame ID: only `A-Z` and `0-9`, 4 characters. Identifiers starting with X, Y, Z are experimental: read and preserve, never create.

---

## 5. Text and Encodings

### 5.1 What Can Be Written

| Tag version | Allowed encoding bytes |
|---|---|
| 2.3 | `$00` ISO-8859-1, `$01` UTF-16 with BOM. **Never write `$02` or `$03` in a 2.3 tag.** |
| 2.4 | `$00`, `$01`, `$02`, `$03` |

### 5.2 Encoding Selection on Write (default; `encoding` parameter may override within §5.1)

1. If the string is entirely in ISO-8859-1 range (characters `$20`–`$FF`, `$0A` where newline is allowed) → `$00`.
2. Otherwise, tag 2.3 → `$01` (UTF-16 LE with BOM `FF FE`).
3. Otherwise, tag 2.4 → `$03` (UTF-8).

Rules:
- Windows-1252 and ISO-8859-1 are not the same: "€", "“”", etc. are outside ISO-8859-1.
- **Never write Cyrillic as bytes in `$00`.** `$00` is ISO-8859-1.
- Numeric strings (TRCK, TPOS, TBPM, TLEN, TDLY, etc.) and URLs are always in `$00`. This is stated in both versions for numeric strings and URLs.
- URL: only ASCII/ISO-8859-1. Non-Latin addresses must be percent-encoded or punycode; do not "fix" them automatically: return a validation error.
- In 2.3 tags, characters outside BMP cannot be represented in UCS-2: by default return an error and suggest 2.4.

### 5.3 UTF-16

- `$01`: BOM is required (`FF FE` or `FE FF`); **all strings in one frame use the same byte order** (2.4 §4); terminator `$00 00`.
- `$02`: UTF-16BE without BOM, terminator `$00 00`.
- An empty string with terminator in `$01` may be written as BOM + `$00 00`.
- Terminator for `$00` and `$03` is one `$00`.

### 5.4 Newlines and Special Characters

- Newline is allowed only where the frame description says `<full text string>`: COMM (text), USLT, USER, etc. Everywhere else it is forbidden. In ISO-8859-1 it is `$0A`.
- There must be no NUL inside a value: it is reserved as terminator/separator.

### 5.5 Reading (Tolerance)

- Read strictly by the encoding byte. Invalid sequences → replace with U+FFFD and return a warning, do not crash.
- Only by explicit `legacy_charset` parameter (e.g. `windows-1251`) is it allowed to treat `$00`-string bytes in that encoding. This is not standard, does not affect writing, and is off by default. Writing such strings back as `$00` is forbidden: on save, re-encode into an allowed encoding (§5.2).
- Some real writers violate the standard (e.g. 2.4 frame sizes not in synchsafe). It is acceptable to guess on read, but writes must always be strict per spec. Mark each such tolerance with `// NON-STANDARD tolerance (read only)`.

---

## 6. Localization

What the standard provides:
- Any T-frame, COMM, USLT, etc. stores Unicode via the encoding byte (§5).
- Frames with a **Language** field (3 bytes, ISO-639-2, lowercase; if language is unknown — `"XXX"`, 2.4 §4): COMM, USLT, SYLT, USER.
- One tag may contain multiple COMM / USLT / SYLT. The pair **(language, content descriptor)** is unique.
- TLAN — language(s) of song text in audio, three-letter ISO-639-2 codes (in 2.4 via separator, e.g. `"eng" $00 "sve" $00`).
- 2.4 only: TSOP, TSOA, TSOT — sort strings instead of artist / album / title.

What the standard **does not** provide:
- T-frames have no language variants. TIT2, TPE1, TALB, and other T-frames are one per tag. You cannot store "Russian title" and "English title" in two TIT2 frames.
- Do not invent replacements (TXXX with custom descriptions, etc.) without owner decision. The tool must return a clear error: "ID3v2 does not define localized variants of frame TIT2; allowed: ...", and list what the standard does allow.

About language codes:
- The standard says "ISO-639-2" without specifying B/T variant (bibliographic/terminological). Accept both, store as received, do not convert.
- Validate against an embedded ISO 639-2 table (download from Library of Congress, `go:embed`). Unknown code → error. Always lowercase.
- Language is the language of content. Encoding is the way characters are written. These parameters are independent.

---

## 7. Covers (APIC)

Structure (2.3 §4.15, 2.4 §4.14):
```
Text encoding  $xx
MIME type      <ISO-8859-1 string> $00
Picture type   $xx
Description    <string per encoding> $00 (00)
Picture data   <binary>
```

Rules:
- MIME: for compatibility `image/png` or `image/jpeg`. Always write the full name (spec allows omitting `image/`; we don't need that).
- Detect format by signature (PNG `89 50 4E 47 0D 0A 1A 0A`, JPEG `FF D8 FF`) and compare with declared MIME. Mismatch is an error, not a guess.
- Do not re-encode, resize, or crop image bytes.
- Picture type: `$03` Cover (front) by default; `$04` Cover (back); full list `$00`–`$14` in 2.3 §4.15 / 2.4 §4.14.
- No more than one picture with type `$01` (32×32 PNG file icon) and no more than one with type `$02`.
- APIC uniqueness: by content descriptor (Description). Two pictures with the same description in one tag are not allowed.
- Description in 2.3 tags: no more than 64 characters.
- Link instead of data: MIME `-->` and full URL. Reading supported; writing only by explicit request (spec: "sparingly").
- Image source for tools: local file path or base64. **No URL downloads** (SSRF). Limit size (set in config; agree default with owner).
- Format size limits: tag size up to 256 MB (28 bits); 2.4 frame also synchsafe 28 bits. Check before writing.

---

## 8. Editing an Existing Tag

1. Version: when editing an existing tag, preserve its version unless conversion is requested. For a new tag — **2.4** (owner requirement to write by the newest standard), `version` parameter may select 2.3.
2. Unknown and unsupported frames: carry over byte-for-byte as they were. Exception: "tag alter preservation" bit = 1 (2.3: bit a in flags byte 1; 2.4: bit a in `%0abc0000`) means the frame is discarded when tag is changed.
3. Frames with read-only, encryption, grouping flags must not be changed. Inform the user.
4. Compressed frames (zlib) can be read; when editing, rebuild without compression; encrypted frames must not be touched.
5. Uniqueness before write (otherwise error, not "silent replacement"):

| Frame | Constraint |
|---|---|
| T*** (except TXXX) | one per tag |
| TXXX, WXXX | one per description |
| W*** (except WXXX) | one per tag, except WCOM and WOAR: multiple, but not with identical content |
| COMM, USLT, SYLT | one per (language, descriptor) |
| USER | 2.3: one per tag; 2.4: one per language |
| APIC | one per description; types `$01`, `$02` — no more than one |
| UFID | one per owner identifier (not empty), identifier up to 64 bytes |
| IPLS (2.3) | one per tag |
| PRIV | multiple, but with different content |

6. Value formats (validate on write):
   - TRCK, TPOS: digits, optionally `/` and digits (`"4/9"`).
   - TBPM, TLEN, TDLY: digits only. TLEN and TDLY in milliseconds.
   - TKEY: no more than 3 characters. TSRC: 12 characters (ISRC).
   - TCOP: starts with year and space.
   - TLAN: ISO-639-2 codes.
   - 2.4 timestamps (TDRC, etc.): `yyyy`, `yyyy-MM`, `yyyy-MM-dd`, `yyyy-MM-ddTHH`, `yyyy-MM-ddTHH:mm`, `yyyy-MM-ddTHH:mm:ss`, all UTC.
   - 2.3 TYER: 4 digits; TDAT: `DDMM`; TIME: `HHMM`.
   - TCON: see table in §4. ID3v1 genre numbers: `0`–`79` defined in both specs; `80`–`125` in 2.3 appendix marked as Winamp extensions.
7. Version conversion. Do only by the differences table (§4) and "id3v2.4.0-changes" document:
   - 2.3 → 2.4: TYER+TDAT+TIME → TDRC; TORY → TDOR; encoding `$01` remains allowed; `/` lists in TCOM, TEXT, TOLY, TOPE, TPE1 → string list.
   - 2.4 → 2.3: TDRC → TYER (+TDAT, TIME if present in value); TDOR → TORY; encodings `$02`, `$03` → `$01`; string list → `/` for frames from §4.
   - Everything not defined by the spec (IPLS ↔ TIPL/TMCL, TDRL, TDEN, TDTG, RVAD ↔ RVA2, EQUA ↔ EQU2, ASPI, SEEK, SIGN, unknown frames) **must not be converted silently**: return a loss report to the user and wait for decision. Until decision, such a frame remains only in the source version (`dry_run` will show this).
8. Padding: write zero padding of reasonable size so subsequent edits fit without rewriting audio (in 2.4 padding is forbidden with footer). If the last byte of the last frame is `$FF` and there is no padding/footer — add padding (otherwise false synchronisation, 2.4 §6.1).
9. Unsynchronisation on write is **not applied by default**. Parser must handle both variants: in 2.3 first reverse unsync over whole tag, in 2.4 — per frame with flag `n`.

---

## 9. File Operations

- Tag lookup: first at file start (`ID3`), then footer `3DI` from file end (before ID3v1, if present), SEEK if encountered. If multiple tags are found, act per 2.4 §5; old tag is discarded if new one has no update flag.
- Write only via temp file in the same folder + `os.Rename`. If new tag fits in old size (frames + padding), do not rewrite audio, change only tag area.
- Copy audio data as a stream, byte-for-byte. After write: reparse result and ensure SHA-256 of audio region before and after matches.
- ID3v1 (128 bytes `TAG` at end of file) must not be changed without explicit request.
- Windows: paths via `filepath`; close files before `os.Rename`; a file opened by another program may fail to rename — return a clear error and do not leave temp file.
- Paths from MCP calls must be validated (`filepath.Clean`, allowed root directories from config, no escape outside them).

---

## 10. MCP: Tool Set (proposal, agree with owner)

- `id3_read` — version, flags, all frames in decoded form. APIC: type, MIME, size, sha256, description, **no** binary data.
- `id3_set_text` — T***, TXXX, W***, WXXX; parameters `version`, `encoding`.
- `id3_set_localized` — COMM / USLT with `language`, `descriptor`, `text`.
- `id3_set_cover` — path or base64, `picture_type`, `description`.
- `id3_extract_cover` — save picture to file.
- `id3_remove` — remove frame(s) by ID / uniqueness key.
- `id3_convert_version` — per §8.7, with loss report.

General requirements:
- All mutating tools have `dry_run` (what will change and which byte frames will be written). For read-only tools set read-only annotation, for destructive ones — destructive (per MCP spec).
- Standard validation errors are returned as tool error with specification section reference. Do not leak panics.

---

## 11. Tests

- Build reference bytes manually by spec and compare byte-for-byte (golden, `testdata/`), separately for each version.
- Required cases:
  - Cyrillic and CJK: 2.3 → `$01` + BOM; 2.4 → `$03`; separately `$02`.
  - Empty strings in `$01` (BOM + `$00 00`); multi-string in 2.4; `/` lists in 2.3.
  - Unsync: test vectors from §4; tag ending with `$FF`.
  - Extended header, CRC, update, restrictions (2.4); footer; padding.
  - APIC: PNG and JPEG; MIME/signature mismatch; two `$01`; duplicate description; `-->`.
  - COMM / USLT: multiple languages, duplicate (language, descriptor), invalid language code.
  - Truncated and corrupted tag; major version above supported; size above 256 MB.
  - Unknown frames: preservation, discard by tag-alter-preservation.
  - 2.3 ↔ 2.4 conversion by table; losses returned as list.
- Fuzz for parser: `go test -fuzz=FuzzParseTag -fuzztime=30s ./internal/id3v2`. Parser must not panic or hang on any input.
- External oracle (if installed): `ffprobe -show_format file.mp3` and Python mutagen library — can they read what we wrote. Analyze mismatch by spec, not by adapting code to oracle.
- "Audio unchanged" property (§9) must be checked in every write integration test.

---

## 12. Commands (PowerShell)

```powershell
gofmt -l .
go vet ./...
go build ./...
go test ./...
```

`go test -race` requires cgo (on Windows needs gcc); run if available.

---

## 13. How an Agent Should Work

1. One frame or one capability at a time. Write a spec-based test first, then code.
2. Before editing, open the relevant section in `docs/spec/` and cite its number in a comment.
3. Do not change unrelated code, do not add dependencies, do not bump Go version in `go.mod`.
4. Record any ambiguity or gap in the standard in `docs/DECISIONS.md` in format "question / what spec says (section) / options / owner decision" and stop at that point.
5. Task is done when: `gofmt -l .` is empty, `go vet ./...` and `go test ./...` are green, tests added for new branches, comments contain spec section references, `docs/DECISIONS.md` is updated.

---

## 14. Open Owner Decisions (proposed values in effect for now)

- [ ] Default version for a new tag: **2.4** (per "newest standard" requirement). Some old players understand 2.3 better.
- [ ] Localized titles (different languages in TIT2/TPE1/TALB): no standard mechanism (§6). Is a custom mechanism needed, and which one?
- [ ] Support reading ID3v2.2 (standard is obsolete).
- [ ] Behavior for characters outside BMP in 2.3 tags: currently error (§5.2).
- [ ] IPLS ↔ TIPL / TMCL mapping on conversion (§8.7).
- [ ] Reading Windows-1251 as legacy mode (§5.5): keep as option or remove.
- [ ] Cover size limit and root directories for file operations (§7, §9).
- [ ] CHAP/CTOC and ATXT addenda: needed?
