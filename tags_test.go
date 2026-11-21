package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/bogem/id3v2/v2"
)

func TestCyrillicV23IsUTF16(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.mp3")
	if err := os.WriteFile(path, []byte("audio-bytes-padding!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	title := "Мир"
	res, err := applyEdit(path, Patch{Title: &title}, editOpt{Confirm: true, DefaultVersion: 3, StripV1: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Wrote || res.Encoding != "UTF-16 (ID3v2.3)" {
		t.Fatalf("result: %+v", res)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte{0xFE, 0xFF, 0x04, 0x1C, 0x04, 0x38, 0x04, 0x40}) {
		t.Fatalf("no UTF-16 for Мир: %x", raw[:min(80, len(raw))])
	}
	if bytes.Contains(raw, []byte{0xD0, 0x9C, 0xD0, 0xB8, 0xD1, 0x80}) {
		t.Fatal("UTF-8 written in v2.3")
	}
	got, err := readTags(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != title || got.Version != 3 {
		t.Fatalf("read back %#v", got)
	}
	if _, err := os.Stat(path + backupSuffix); err != nil {
		t.Fatal("no backup", err)
	}
}

func TestForceV24UTF8AndPreviewDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.mp3")
	if err := os.WriteFile(path, []byte("audio-bytes-padding!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	title := "Мир"
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	res, err := applyEdit(path, Patch{Title: &title}, editOpt{Confirm: false, ForceVersion: 4, DefaultVersion: 3})
	if err != nil {
		t.Fatal(err)
	}
	if res.Wrote {
		t.Fatal("preview should not write")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("preview modified file")
	}
	res, err = applyEdit(path, Patch{Title: &title}, editOpt{Confirm: true, ForceVersion: 4, DefaultVersion: 3, StripV1: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !bytes.Contains(raw, []byte{0xD0, 0x9C, 0xD0, 0xB8, 0xD1, 0x80}) {
		t.Fatalf("no UTF-8: %x", raw[:min(80, len(raw))])
	}
	got, err := readTags(path)
	if err != nil || got.Title != title || got.Version != 4 {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestCoverRoundTripAndJail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.mp3")
	img := filepath.Join(dir, "cover.jpg")
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xD9}
	if err := os.WriteFile(path, append([]byte("audio-bytes-padding!!"), []byte("TAG"+string(make([]byte, 125)))...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(img, jpeg, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := setCover(path, img, "Cover", false, true, true, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Wrote || !res.Stripped {
		t.Fatalf("%+v", res)
	}
	v1, err := hasID3v1(path)
	if err != nil || v1 {
		t.Fatal("ID3v1 should be stripped", v1, err)
	}
	out, info, err := extractCover(path, "", false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, jpeg) || info.Bytes != len(jpeg) {
		t.Fatalf("cover mismatch: %d", info.Bytes)
	}

	j := Jail{Root: dir}
	if _, err := j.Resolve(filepath.Join(dir, "..", "etc", "passwd")); err == nil {
		t.Fatal("jail allowed escaping upward")
	}
	outside := filepath.Join(filepath.Dir(dir), "evil.mp3")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.mp3")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := j.OpenMP3(link, 1000); err == nil {
		t.Fatal("jail followed symlink outside")
	}
}

func TestMojibakeRepair(t *testing.T) {
	raw := []byte{0xD0, 0x9C, 0xD0, 0xB8, 0xD1, 0x80}
	broken := "\u00d0\u0153\u00d0\u00b8\u00d1\u20ac" // cp1252 view of those UTF-8 bytes
	if string(raw) != "Мир" {
		t.Fatal(string(raw))
	}
	fixed, ok := repairMojibake(broken)
	if !ok || fixed != "Мир" {
		t.Fatalf("ok=%v fixed=%q broken=%q", ok, fixed, broken)
	}
	if _, ok := repairMojibake("Мир"); ok {
		t.Fatal("real Cyrillic should not be touched")
	}

	path := filepath.Join(t.TempDir(), "d.mp3")
	if err := os.WriteFile(path, []byte("audio-bytes-padding!!"), 0o644); err != nil {
		t.Fatal(err)
	}
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatal(err)
	}
	tag.SetVersion(3)
	tag.SetDefaultEncoding(id3v2.EncodingUTF16)
	tag.SetTitle(broken)
	if err := tag.Save(); err != nil {
		t.Fatal(err)
	}
	tag.Close()

	res, err := applyEdit(path, Patch{}, editOpt{Confirm: false, RepairMojibake: true, DefaultVersion: 3})
	if err != nil {
		t.Fatal(err)
	}
	if res.After.Title != "Мир" || res.Wrote {
		t.Fatalf("%+v", res)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
