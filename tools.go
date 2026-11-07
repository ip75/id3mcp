package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Config holds the application configuration.
type Config struct {
	Root           string
	MaxFiles       int
	MaxBytes       int64
	MaxImage       int64
	DefaultVersion byte
	StripV1        bool
}

// App represents the main application.
type app struct {
	cfg Config
}

// boolPtr returns a pointer to a boolean value.
func boolPtr(v bool) *bool { return &v }

// instructions returns the application instructions.
func (a *app) instructions() string {
	return fmt.Sprintf(`Working directory only %s. Archive music does not belong here.
First id3_list / id3_read. Then id3_set or id3_set_cover with confirm=false and show diff to person.
confirm=true writes to disk. No more than %d files per call. No recursion.
New file gets ID3v2.3 and UTF-16. UTF-8 is only written in ID3v2.4 (force_version=4 or file already 2.4).
ID3v2.3 specification does not support UTF-8. Empty string clears field. Don't put cover in text.`, a.cfg.Root, a.cfg.MaxFiles)
}

// register registers all MCP tools with the server.
func (a *app) register(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "id3_list",
		Description: "List .mp3 files in one directory inside working folder. No recursion, no archive.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, a.list)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "id3_read",
		Description: "Read tags and cover summary (without image bytes). Paths only inside working folder.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, a.read)
	mcp.AddTool(server, &mcp.Tool{
		Name: "id3_set",
		Description: "Edit text ID3 tags of batch files. confirm=false writes nothing and is required first step. " +
			"Empty string clears field. track/disc: \"3\" or \"3/12\". Don't call on archive directory.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, a.set)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "id3_set_cover",
		Description: "Set front cover from .jpg or .png in same working folder. confirm=false only preview.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, a.setCover)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "id3_extract_cover",
		Description: "Extract cover to file inside working folder. Won't overwrite foreign covers without overwrite=true.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
	}, a.extractCover)
}

// ListIn represents input for the id3_list tool.
type listIn struct {
	Path string `json:"path" jsonschema:"directory inside working folder, can be dot"`
}

// ListOut represents output for the id3_list tool.
type listOut struct {
	Root      string   `json:"root"`
	Dir       string   `json:"dir"`
	Files     []string `json:"files"`
	Truncated bool     `json:"truncated,omitempty"`
}

// list lists MP3 files in a directory.
func (a *app) list(ctx context.Context, _ *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, listOut, error) {
	j := Jail{a.cfg.Root}
	dir, err := j.Resolve(in.Path)
	if err != nil {
		return toolErr[listOut](err)
	}
	st, err := os.Stat(dir)
	if err != nil {
		return toolErr[listOut](err)
	}
	if !st.IsDir() {
		return toolErr[listOut](fmt.Errorf("%s not directory", dir))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return toolErr[listOut](err)
	}
	var files []string
	truncated := false
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".mp3") {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if _, _, err := j.OpenMP3(full, a.cfg.MaxBytes); err != nil {
			continue
		}
		rel, err := filepath.Rel(a.cfg.Root, full)
		if err != nil {
			continue
		}
		files = append(files, rel)
		if len(files) >= 200 {
			truncated = true
			break
		}
	}
	sort.Strings(files)
	if files == nil {
		files = []string{}
	}
	return toolOK(listOut{Root: a.cfg.Root, Dir: dir, Files: files, Truncated: truncated})
}

// ReadIn represents input for the id3_read tool.
type readIn struct {
	Paths []string `json:"paths" jsonschema:"paths to mp3, absolute or relative to working folder"`
}

// ReadOut represents output for the id3_read tool.
type readOut struct {
	Files []FileTags `json:"files"`
}

// read reads ID3 tags from files.
func (a *app) read(ctx context.Context, _ *mcp.CallToolRequest, in readIn) (*mcp.CallToolResult, readOut, error) {
	if err := a.limit(len(in.Paths)); err != nil {
		return toolErr[readOut](err)
	}
	out := readOut{Files: []FileTags{}}
	j := Jail{a.cfg.Root}
	for _, p := range in.Paths {
		real, _, err := j.OpenMP3(p, a.cfg.MaxBytes)
		if err != nil {
			out.Files = append(out.Files, FileTags{Path: p})
			return toolErrMsg[readOut](err.Error(), out)
		}
		tags, err := readTags(real)
		if err != nil {
			return toolErr[readOut](err)
		}
		out.Files = append(out.Files, tags)
	}
	return toolOK(out)
}

// SetIn represents input for the id3_set tool.
type setIn struct {
	Confirm        bool       `json:"confirm" jsonschema:"true writes to disk. false, by default, only shows diff"`
	ForceVersion   int        `json:"force_version,omitempty" jsonschema:"3 = UTF-16, 4 = UTF-8. Zero leaves file version unchanged"`
	RepairMojibake bool       `json:"repair_mojibake,omitempty" jsonschema:"fix text that is actually UTF-8, read as cp1252"`
	Files          []fileEdit `json:"files" jsonschema:"which files and what fields to change"`
}

// FileEdit represents a file edit operation.
type fileEdit struct {
	Path        string  `json:"path"`
	Title       *string `json:"title,omitempty"`
	Artist      *string `json:"artist,omitempty"`
	Album       *string `json:"album,omitempty"`
	AlbumArtist *string `json:"album_artist,omitempty"`
	Genre       *string `json:"genre,omitempty"`
	Year        *string `json:"year,omitempty"`
	Track       *string `json:"track,omitempty"`
	Disc        *string `json:"disc,omitempty"`
	Composer    *string `json:"composer,omitempty"`
	Comment     *string `json:"comment,omitempty"`
}

// SetOut represents output for the id3_set tool.
type setOut struct {
	Wrote bool         `json:"wrote"`
	Files []EditResult `json:"files"`
}

// set edits ID3 tags in files.
func (a *app) set(ctx context.Context, _ *mcp.CallToolRequest, in setIn) (*mcp.CallToolResult, setOut, error) {
	if err := a.limit(len(in.Files)); err != nil {
		return toolErr[setOut](err)
	}
	if in.ForceVersion != 0 && in.ForceVersion != 3 && in.ForceVersion != 4 {
		return toolErr[setOut](fmt.Errorf("force_version only 3 or 4"))
	}
	out := setOut{Files: []EditResult{}}
	j := Jail{a.cfg.Root}
	anyWrote := false
	for _, f := range in.Files {
		real, _, err := j.OpenMP3(f.Path, a.cfg.MaxBytes)
		if err != nil {
			out.Files = append(out.Files, EditResult{Path: f.Path, Error: err.Error()})
			continue
		}
		res, err := applyEdit(real, Patch{
			Title: f.Title, Artist: f.Artist, Album: f.Album, AlbumArtist: f.AlbumArtist,
			Genre: f.Genre, Year: f.Year, Track: f.Track, Disc: f.Disc,
			Composer: f.Composer, Comment: f.Comment,
		}, editOpt{
			Confirm:        in.Confirm,
			ForceVersion:   in.ForceVersion,
			RepairMojibake: in.RepairMojibake,
			StripV1:        a.cfg.StripV1,
			DefaultVersion: a.cfg.DefaultVersion,
		})
		if err != nil {
			res.Path = real
			res.Error = err.Error()
		}
		if res.Wrote {
			anyWrote = true
		}
		out.Files = append(out.Files, res)
	}
	out.Wrote = anyWrote
	return toolOK(out)
}

// CoverItem represents a cover file operation.
type coverItem struct {
	Path        string `json:"path"`
	ImagePath   string `json:"image_path"`
	Description string `json:"description,omitempty"`
	ReplaceAll  bool   `json:"replace_all,omitempty"`
}

// CoverIn represents input for the id3_set_cover tool.
type coverIn struct {
	Confirm bool        `json:"confirm" jsonschema:"true writes to disk"`
	Files   []coverItem `json:"files"`
}

// setCover sets covers for files.
func (a *app) setCover(ctx context.Context, _ *mcp.CallToolRequest, in coverIn) (*mcp.CallToolResult, setOut, error) {
	if err := a.limit(len(in.Files)); err != nil {
		return toolErr[setOut](err)
	}
	out := setOut{Files: []EditResult{}}
	j := Jail{a.cfg.Root}
	anyWrote := false
	for _, f := range in.Files {
		real, _, err := j.OpenMP3(f.Path, a.cfg.MaxBytes)
		if err != nil {
			out.Files = append(out.Files, EditResult{Path: f.Path, Error: err.Error()})
			continue
		}
		img, _, err := j.OpenRegular(f.ImagePath, a.cfg.MaxImage, ".jpg", ".jpeg", ".png")
		if err != nil {
			out.Files = append(out.Files, EditResult{Path: real, Error: err.Error()})
			continue
		}
		res, err := setCover(real, img, f.Description, f.ReplaceAll, in.Confirm, a.cfg.StripV1, a.cfg.DefaultVersion)
		if err != nil {
			res.Path = real
			res.Error = err.Error()
		}
		if res.Wrote {
			anyWrote = true
		}
		out.Files = append(out.Files, res)
	}
	out.Wrote = anyWrote
	return toolOK(out)
}

// ExtractIn represents input for the id3_extract_cover tool.
type extractIn struct {
	Path      string `json:"path"`
	OutPath   string `json:"out_path,omitempty"`
	Overwrite bool   `json:"overwrite,omitempty"`
}

// ExtractOut represents output for the id3_extract_cover tool.
type extractOut struct {
	Path  string    `json:"path"`
	Cover CoverInfo `json:"cover"`
}

// extractCover extracts covers from files.
func (a *app) extractCover(ctx context.Context, _ *mcp.CallToolRequest, in extractIn) (*mcp.CallToolResult, extractOut, error) {
	j := Jail{a.cfg.Root}
	real, _, err := j.OpenMP3(in.Path, a.cfg.MaxBytes)
	if err != nil {
		return toolErr[extractOut](err)
	}
	out := in.OutPath
	if out != "" {
		resolved, err := j.Resolve(out)
		if err != nil {
			return toolErr[extractOut](err)
		}
		parent := filepath.Dir(resolved)
		st, err := os.Stat(parent)
		if err != nil || !st.IsDir() {
			return toolErr[extractOut](fmt.Errorf("cover directory does not exist"))
		}
		out = resolved
	}
	path, info, err := extractCover(real, out, in.Overwrite)
	if err != nil {
		return toolErr[extractOut](err)
	}
	// extractCover with empty out writes next to the mp3, which is inside the jail.
	if _, err := j.Resolve(path); err != nil {
		os.Remove(path)
		return toolErr[extractOut](err)
	}
	return toolOK(extractOut{Path: path, Cover: info})
}

// limit checks if the number of files is within limits.
func (a *app) limit(n int) error {
	if n < 1 {
		return fmt.Errorf("at least one file needed")
	}
	if n > a.cfg.MaxFiles {
		return fmt.Errorf("can process %d files at once, got %d. Split into batches", a.cfg.MaxFiles, n)
	}
	return nil
}

// toolOK returns a successful tool result.
func toolOK[T any](v T) (*mcp.CallToolResult, T, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, v, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, v, nil
}

// toolErr returns an error tool result.
func toolErr[T any](err error) (*mcp.CallToolResult, T, error) {
	var zero T
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}, zero, nil
}

// toolErrMsg returns an error tool result with additional message.
func toolErrMsg[T any](msg string, v T) (*mcp.CallToolResult, T, error) {
	b, _ := json.MarshalIndent(v, "", "  ")
	text := msg
	if len(b) > 0 {
		text = msg + "\n" + string(b)
	}
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, v, nil
}
