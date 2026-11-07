package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bogem/id3v2/v2"
)

const backupSuffix = ".id3mcp.bak"

var (
	numRE  = regexp.MustCompile(`^(?:0*[1-9][0-9]{0,2})(?:/0*[1-9][0-9]{0,2})?$`)
	yearRE = regexp.MustCompile(`^[0-9]{4}([./-][0-9A-Za-z./-]{0,16})?$`)
)

// FileTags represents the tags and cover information of an MP3 file.
type FileTags struct {
	Path        string      `json:"path"`
	Version     int         `json:"version"`
	Encoding    string      `json:"encoding"`
	Title       string      `json:"title"`
	Artist      string      `json:"artist"`
	Album       string      `json:"album"`
	AlbumArtist string      `json:"album_artist"`
	Genre       string      `json:"genre"`
	Year        string      `json:"year"`
	Track       string      `json:"track"`
	Disc        string      `json:"disc"`
	Composer    string      `json:"composer"`
	Comment     string      `json:"comment"`
	Covers      []CoverInfo `json:"covers"`
	HasID3v1    bool        `json:"has_id3v1"`
}

// CoverInfo represents information about a cover image.
type CoverInfo struct {
	Type        int    `json:"type"`
	TypeName    string `json:"type_name"`
	MIME        string `json:"mime"`
	Bytes       int    `json:"bytes"`
	Description string `json:"description,omitempty"`
}

// Patch represents changes to be applied to ID3 tags.
type Patch struct {
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

// EditResult represents the result of an edit operation.
type EditResult struct {
	Path     string   `json:"path"`
	Wrote    bool     `json:"wrote"`
	Backup   string   `json:"backup,omitempty"`
	Changed  []string `json:"changed,omitempty"`
	Version  int      `json:"version"`
	Encoding string   `json:"encoding"`
	Before   FileTags `json:"before"`
	After    FileTags `json:"after"`
	Stripped bool     `json:"stripped_id3v1,omitempty"`
	Repaired []string `json:"repaired_mojibake,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// encodingFor returns the appropriate ID3 encoding for a version.
func encodingFor(version byte) id3v2.Encoding {
	if version >= 4 {
		return id3v2.EncodingUTF8
	}
	return id3v2.EncodingUTF16
}

// encodingName returns the name of an encoding for a given version.
func encodingName(v byte) string {
	if v >= 4 {
		return "UTF-8 (ID3v2.4)"
	}
	return "UTF-16 (ID3v2.3)"
}

// readTags reads ID3 tags from a file.
func readTags(path string) (FileTags, error) {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return FileTags{}, err
	}
	defer tag.Close()
	out := snapshot(path, tag)
	has, err := fileHasID3(path)
	if err != nil {
		return FileTags{}, err
	}
	if !has {
		out.Version = 0
		out.Encoding = ""
	}
	v1, err := hasID3v1(path)
	if err != nil {
		return FileTags{}, err
	}
	out.HasID3v1 = v1
	return out, nil
}

// snapshot creates a snapshot of the current state of tags.
func snapshot(path string, tag *id3v2.Tag) FileTags {
	ver := int(tag.Version())
	out := FileTags{
		Path:        path,
		Version:     ver,
		Encoding:    encodingName(byte(ver)),
		Title:       textOf(tag, "TIT2"),
		Artist:      textOf(tag, "TPE1"),
		Album:       textOf(tag, "TALB"),
		AlbumArtist: textOf(tag, "TPE2"),
		Genre:       textOf(tag, "TCON"),
		Year:        firstText(tag, "TDRC", "TYER"),
		Track:       textOf(tag, "TRCK"),
		Disc:        textOf(tag, "TPOS"),
		Composer:    textOf(tag, "TCOM"),
		Comment:     commentText(tag),
	}
	for _, f := range tag.GetFrames("APIC") {
		pic, ok := f.(id3v2.PictureFrame)
		if !ok {
			continue
		}
		out.Covers = append(out.Covers, CoverInfo{
			Type:        int(pic.PictureType),
			TypeName:    pictureTypeName(pic.PictureType),
			MIME:        pic.MimeType,
			Bytes:       len(pic.Picture),
			Description: pic.Description,
		})
	}
	if out.Covers == nil {
		out.Covers = []CoverInfo{}
	}
	return out
}

// textOf gets the text value of a tag frame.
func textOf(tag *id3v2.Tag, id string) string {
	f := tag.GetLastFrame(id)
	if f == nil {
		return ""
	}
	tf, ok := f.(id3v2.TextFrame)
	if !ok {
		return ""
	}
	return strings.TrimRight(tf.Text, "\x00")
}

// firstText gets the first non-empty text value from multiple tag frames.
func firstText(tag *id3v2.Tag, ids ...string) string {
	for _, id := range ids {
		if s := textOf(tag, id); s != "" {
			return s
		}
	}
	return ""
}

// commentText gets the comment text from all COMM frames.
func commentText(tag *id3v2.Tag) string {
	var parts []string
	for _, f := range tag.GetFrames("COMM") {
		c, ok := f.(id3v2.CommentFrame)
		if !ok {
			continue
		}
		t := strings.TrimRight(c.Text, "\x00")
		if t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n")
}

// applyEdit applies a patch to ID3 tags in a file.
func applyEdit(path string, patch Patch, opt editOpt) (EditResult, error) {
	res := EditResult{Path: path}
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return res, err
	}
	defer tag.Close()

	before := snapshot(path, tag)
	hasTag, err := fileHasID3(path)
	if err != nil {
		return res, err
	}
	if !hasTag {
		before.Version = 0
		before.Encoding = ""
	}
	v1, err := hasID3v1(path)
	if err != nil {
		return res, err
	}
	before.HasID3v1 = v1
	res.Before = before

	version, err := decideVersion(tag, hasTag, opt.DefaultVersion, opt.ForceVersion)
	if err != nil {
		return res, err
	}
	tag.SetVersion(version)
	enc := encodingFor(version)
	tag.SetDefaultEncoding(enc)

	merged := before
	var repaired []string
	set := func(name string, next *string, dst *string) error {
		if next == nil {
			return nil
		}
		v := *next
		if opt.RepairMojibake {
			if fixed, ok := repairMojibake(v); ok {
				repaired = append(repaired, name)
				v = fixed
			}
		}
		if err := validateText(name, v); err != nil {
			return err
		}
		*dst = v
		return nil
	}
	if err := set("title", patch.Title, &merged.Title); err != nil {
		return res, err
	}
	if err := set("artist", patch.Artist, &merged.Artist); err != nil {
		return res, err
	}
	if err := set("album", patch.Album, &merged.Album); err != nil {
		return res, err
	}
	if err := set("album_artist", patch.AlbumArtist, &merged.AlbumArtist); err != nil {
		return res, err
	}
	if err := set("genre", patch.Genre, &merged.Genre); err != nil {
		return res, err
	}
	if err := set("year", patch.Year, &merged.Year); err != nil {
		return res, err
	}
	if err := set("track", patch.Track, &merged.Track); err != nil {
		return res, err
	}
	if err := set("disc", patch.Disc, &merged.Disc); err != nil {
		return res, err
	}
	if err := set("composer", patch.Composer, &merged.Composer); err != nil {
		return res, err
	}
	if err := set("comment", patch.Comment, &merged.Comment); err != nil {
		return res, err
	}

	if opt.RepairMojibake {
		repairField := func(name string, dst *string) {
			if fixed, ok := repairMojibake(*dst); ok {
				repaired = append(repaired, name)
				*dst = fixed
			}
		}
		repairField("title", &merged.Title)
		repairField("artist", &merged.Artist)
		repairField("album", &merged.Album)
		repairField("album_artist", &merged.AlbumArtist)
		repairField("genre", &merged.Genre)
		repairField("composer", &merged.Composer)
		repairField("comment", &merged.Comment)
	}

	changed := diffTags(before, merged)
	versionChanged := int(version) != before.Version && before.Version != 0
	if versionChanged {
		changed = append(changed, "version")
	}
	res.Changed = changed
	res.Repaired = repaired
	res.Version = int(version)
	res.Encoding = encodingName(version)

	after := merged
	after.Path = path
	after.Version = int(version)
	after.Encoding = encodingName(version)
	after.Covers = before.Covers
	after.HasID3v1 = before.HasID3v1 && !opt.StripV1
	res.After = after

	needStrip := opt.StripV1 && before.HasID3v1
	if len(changed) == 0 {
		if opt.Confirm && needStrip {
			bak, err := ensureBackup(path)
			if err != nil {
				return res, err
			}
			res.Backup = bak
			stripped, err := stripID3v1(path)
			if err != nil {
				return res, err
			}
			res.Wrote = stripped
			res.Stripped = stripped
			if stripped {
				res.Changed = []string{"id3v1"}
			}
		}
		return res, nil
	}
	if !opt.Confirm {
		return res, nil
	}

	// Rewrite every managed text frame when the version changes so TYER/TDRC
	// follow the version. Otherwise only the fields that actually differ.
	rewriteAll := versionChanged
	writeText := func(id, val string, differ bool) {
		if !rewriteAll && !differ {
			return
		}
		tag.DeleteFrames(id)
		if val != "" {
			tag.AddTextFrame(id, enc, val)
		}
	}
	writeText("TIT2", merged.Title, before.Title != merged.Title)
	writeText("TPE1", merged.Artist, before.Artist != merged.Artist)
	writeText("TALB", merged.Album, before.Album != merged.Album)
	writeText("TPE2", merged.AlbumArtist, before.AlbumArtist != merged.AlbumArtist)
	writeText("TCON", merged.Genre, before.Genre != merged.Genre)
	writeText("TCOM", merged.Composer, before.Composer != merged.Composer)
	writeText("TRCK", merged.Track, before.Track != merged.Track)
	writeText("TPOS", merged.Disc, before.Disc != merged.Disc)
	if rewriteAll || before.Year != merged.Year {
		tag.DeleteFrames("TYER")
		tag.DeleteFrames("TDRC")
		if merged.Year != "" {
			id := "TYER"
			if version >= 4 {
				id = "TDRC"
			}
			tag.AddTextFrame(id, enc, merged.Year)
		}
	}
	if rewriteAll || before.Comment != merged.Comment {
		tag.DeleteFrames("COMM")
		if merged.Comment != "" {
			lang := "eng"
			if hasCyrillic(merged.Comment) {
				lang = "rus"
			}
			tag.AddCommentFrame(id3v2.CommentFrame{
				Encoding: enc,
				Language: lang,
				Text:     merged.Comment,
			})
		}
	}

	bak, err := ensureBackup(path)
	if err != nil {
		return res, err
	}
	res.Backup = bak
	if err := tag.Save(); err != nil {
		return res, err
	}
	res.Wrote = true
	if opt.StripV1 {
		stripped, err := stripID3v1(path)
		if err != nil {
			return res, err
		}
		res.Stripped = stripped
		res.After.HasID3v1 = before.HasID3v1 && !stripped
	}
	return res, nil
}

// editOpt represents options for editing ID3 tags.
type editOpt struct {
	Confirm        bool
	ForceVersion   int
	RepairMojibake bool
	StripV1        bool
	DefaultVersion byte
}

// decideVersion decides which version to use for the tag.
func decideVersion(tag *id3v2.Tag, hasTag bool, def byte, force int) (byte, error) {
	if force == 3 || force == 4 {
		return byte(force), nil
	}
	if def < 3 || def > 4 {
		def = 3
	}
	if !hasTag {
		return def, nil
	}
	v := tag.Version()
	if v < 3 {
		return def, nil
	}
	return v, nil
}

// fileHasID3 checks if a file has ID3 tags.
func fileHasID3(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, 3)
	_, err = io.ReadFull(f, buf)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return string(buf) == "ID3", nil
}

// diffTags compares two FileTags and returns the changed fields.
func diffTags(a, b FileTags) []string {
	var c []string
	pair := func(name, x, y string) {
		if x != y {
			c = append(c, name)
		}
	}
	pair("title", a.Title, b.Title)
	pair("artist", a.Artist, b.Artist)
	pair("album", a.Album, b.Album)
	pair("album_artist", a.AlbumArtist, b.AlbumArtist)
	pair("genre", a.Genre, b.Genre)
	pair("year", a.Year, b.Year)
	pair("track", a.Track, b.Track)
	pair("disc", a.Disc, b.Disc)
	pair("composer", a.Composer, b.Composer)
	pair("comment", a.Comment, b.Comment)
	return c
}

// validateText validates text field values.
func validateText(name, v string) error {
	if strings.ContainsRune(v, '\x00') || strings.ContainsAny(v, "\r\n") && name != "comment" {
		return fmt.Errorf("%s: newlines and NUL not allowed", name)
	}
	limit := 512
	if name == "comment" {
		limit = 2000
	}
	if utf8.RuneCountInString(v) > limit {
		return fmt.Errorf("%s: longer than %d characters", name, limit)
	}
	switch name {
	case "year":
		if v != "" && !yearRE.MatchString(v) {
			return fmt.Errorf("year: expect year like 1994, got %q", v)
		}
	case "track", "disc":
		if v != "" && !numRE.MatchString(v) {
			return fmt.Errorf("%s: expect 3 or 3/12, got %q", name, v)
		}
	}
	return nil
}

// setCover sets a cover image for an MP3 file.
func setCover(path, imagePath, description string, replaceAll, confirm, stripV1 bool, defVersion byte) (EditResult, error) {
	res := EditResult{Path: path}
	data, mime, err := readImage(imagePath)
	if err != nil {
		return res, err
	}
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return res, err
	}
	defer tag.Close()

	before := snapshot(path, tag)
	hasTag, err := fileHasID3(path)
	if err != nil {
		return res, err
	}
	if !hasTag {
		before.Version = 0
		before.Encoding = ""
	}
	v1, err := hasID3v1(path)
	if err != nil {
		return res, err
	}
	before.HasID3v1 = v1
	res.Before = before

	version, err := decideVersion(tag, hasTag, defVersion, 0)
	if err != nil {
		return res, err
	}
	tag.SetVersion(version)
	enc := encodingFor(version)
	tag.SetDefaultEncoding(enc)

	if description == "" {
		description = "Cover"
	}
	if err := validateText("description", description); err != nil {
		return res, err
	}

	same := false
	for _, f := range tag.GetFrames("APIC") {
		pic, ok := f.(id3v2.PictureFrame)
		if !ok {
			continue
		}
		if !replaceAll && pic.PictureType != id3v2.PTFrontCover {
			continue
		}
		if pic.MimeType == mime && bytes.Equal(pic.Picture, data) {
			same = true
		} else {
			same = false
			break
		}
	}
	fronts := 0
	for _, c := range before.Covers {
		if replaceAll || c.Type == int(id3v2.PTFrontCover) {
			fronts++
		}
	}
	if fronts == 0 {
		same = false
	}
	res.Version = int(version)
	res.Encoding = encodingName(version)
	after := before
	after.Version = int(version)
	after.Encoding = encodingName(version)
	if !same {
		res.Changed = []string{"cover"}
		kept := []CoverInfo{}
		if !replaceAll {
			for _, c := range before.Covers {
				if c.Type != int(id3v2.PTFrontCover) {
					kept = append(kept, c)
				}
			}
		}
		kept = append(kept, CoverInfo{
			Type:        int(id3v2.PTFrontCover),
			TypeName:    pictureTypeName(id3v2.PTFrontCover),
			MIME:        mime,
			Bytes:       len(data),
			Description: description,
		})
		after.Covers = kept
	}
	res.After = after
	if same || !confirm {
		return res, nil
	}

	var kept []id3v2.PictureFrame
	for _, f := range tag.GetFrames("APIC") {
		pic, ok := f.(id3v2.PictureFrame)
		if !ok {
			continue
		}
		if replaceAll || pic.PictureType == id3v2.PTFrontCover {
			continue
		}
		kept = append(kept, pic)
	}
	tag.DeleteFrames("APIC")
	for _, pic := range kept {
		tag.AddAttachedPicture(pic)
	}
	tag.AddAttachedPicture(id3v2.PictureFrame{
		Encoding:    enc,
		MimeType:    mime,
		PictureType: id3v2.PTFrontCover,
		Description: description,
		Picture:     data,
	})

	bak, err := ensureBackup(path)
	if err != nil {
		return res, err
	}
	res.Backup = bak
	if err := tag.Save(); err != nil {
		return res, err
	}
	res.Wrote = true
	if stripV1 {
		stripped, err := stripID3v1(path)
		if err != nil {
			return res, err
		}
		res.Stripped = stripped
	}
	return res, nil
}

// extractCover extracts a cover image from an MP3 file.
func extractCover(path, outPath string, overwrite bool) (string, CoverInfo, error) {
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		return "", CoverInfo{}, err
	}
	defer tag.Close()

	var pic *id3v2.PictureFrame
	for _, f := range tag.GetFrames("APIC") {
		p, ok := f.(id3v2.PictureFrame)
		if !ok || len(p.Picture) == 0 {
			continue
		}
		cp := p
		if p.PictureType == id3v2.PTFrontCover {
			pic = &cp
			break
		}
		if pic == nil {
			pic = &cp
		}
	}
	if pic == nil {
		return "", CoverInfo{}, fmt.Errorf("file has no cover")
	}
	ext := ".bin"
	switch strings.ToLower(pic.MimeType) {
	case "image/jpeg", "image/jpg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	}
	if outPath == "" {
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		outPath = filepath.Join(filepath.Dir(path), base+"-cover"+ext)
	}
	if !overwrite {
		if _, err := os.Stat(outPath); err == nil {
			return "", CoverInfo{}, fmt.Errorf("%s already exists, overwrite=true to replace", outPath)
		}
	}
	if err := os.WriteFile(outPath, pic.Picture, 0o644); err != nil {
		return "", CoverInfo{}, err
	}
	info := CoverInfo{
		Type:        int(pic.PictureType),
		TypeName:    pictureTypeName(pic.PictureType),
		MIME:        pic.MimeType,
		Bytes:       len(pic.Picture),
		Description: pic.Description,
	}
	return outPath, info, nil
}

// readImage reads and validates an image file.
func readImage(path string) ([]byte, string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	var mime string
	switch ext {
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".png":
		mime = "image/png"
	default:
		return nil, "", fmt.Errorf("cover only .jpg or .png")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, "", err
	}
	if mime == "image/jpeg" && !bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}) {
		return nil, "", fmt.Errorf("%s: not JPEG", filepath.Base(path))
	}
	if mime == "image/png" && !bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}) {
		return nil, "", fmt.Errorf("%s: not PNG", filepath.Base(path))
	}
	return data, mime, nil
}

// ensureBackup creates a backup of the file.
func ensureBackup(path string) (string, error) {
	bak := path + backupSuffix
	if _, err := os.Stat(bak); err == nil {
		return bak, nil
	}
	in, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return "", err
	}
	tmp := bak + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, st.Mode().Perm())
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(tmp)
		return "", copyErr
	}
	if closeErr != nil {
		os.Remove(tmp)
		return "", closeErr
	}
	if err := os.Rename(tmp, bak); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return bak, nil
}

// hasID3v1 checks if a file has ID3v1 tags.
func hasID3v1(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false, err
	}
	if st.Size() < 128 {
		return false, nil
	}
	buf := make([]byte, 3)
	if _, err := f.ReadAt(buf, st.Size()-128); err != nil {
		return false, err
	}
	return string(buf) == "TAG", nil
}

// stripID3v1 removes ID3v1 tags from a file.
func stripID3v1(path string) (bool, error) {
	ok, err := hasID3v1(path)
	if err != nil || !ok {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false, err
	}
	if err := f.Truncate(st.Size() - 128); err != nil {
		return false, err
	}
	return true, nil
}

// pictureTypeName returns the name of a picture type.
func pictureTypeName(t byte) string {
	names := map[byte]string{
		0: "other", 1: "file icon", 3: "front cover", 4: "back cover",
		5: "leaflet", 6: "media", 7: "lead artist", 8: "artist",
	}
	if n, ok := names[t]; ok {
		return n
	}
	return fmt.Sprintf("type %d", t)
}

// hasCyrillic checks if a string contains Cyrillic characters.
func hasCyrillic(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

// repairMojibake undoes the usual damage: UTF-8 bytes read as Windows-1252
// and stored back ("Мир" shown as "ÐœÐ¸Ñ€"). It does not touch real Cyrillic.
func repairMojibake(s string) (string, bool) {
	if s == "" || hasCyrillic(s) {
		return s, false
	}
	b, ok := encodeCP1252(s)
	if !ok || !utf8.Valid(b) {
		return s, false
	}
	fixed := string(b)
	if fixed == s || !hasCyrillic(fixed) {
		return s, false
	}
	return fixed, true
}

// cp1252 byte -> rune for the 0x80-0x9F range that differs from Latin-1.
var cp1252Extra = map[byte]rune{
	0x80: '€', 0x82: '‚', 0x83: 'ƒ', 0x84: '„', 0x85: '…',
	0x86: '†', 0x87: '‡', 0x88: 'ˆ', 0x89: '‰', 0x8A: 'Š',
	0x8B: '‹', 0x8C: 'Œ', 0x8E: 'Ž', 0x91: '‘', 0x92: '’',
	0x93: '“', 0x94: '”', 0x95: '•', 0x96: '–', 0x97: '—',
	0x98: '˜', 0x99: '™', 0x9A: 'š', 0x9B: '›', 0x9C: 'œ',
	0x9E: 'ž', 0x9F: 'Ÿ',
}

// encodeCP1252 encodes a string as CP1252 bytes.
func encodeCP1252(s string) ([]byte, bool) {
	rev := map[rune]byte{}
	for b, r := range cp1252Extra {
		rev[r] = b
	}
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if b, ok := rev[r]; ok {
			out = append(out, b)
			continue
		}
		if r < 0x80 || (r >= 0xA0 && r <= 0xFF) {
			out = append(out, byte(r))
			continue
		}
		return nil, false
	}
	return out, true
}
