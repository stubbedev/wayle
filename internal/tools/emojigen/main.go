// Command emojigen extracts GTK's emoji tables for the launcher's emoji
// mode. GTK compiles them into libgtk-4 as the GResource entries
// /org/gtk/libgtk/emoji/<lang>.data (GVariant a(aussasasu), per
// gtkemojichooser.c); the Rust shell reads them from the loaded
// library, and the Go shell, which loads no GTK, embeds them instead.
//
//	go run ./internal/tools/emojigen /path/to/libgtk-4.so.1 service/launcher/modes/emojidata
//
// Each table is written gzip-compressed as <lang>.data.gz.
package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/stubbedev/wayle/internal/gvariant"
)

const prefix = "/org/gtk/libgtk/emoji/"

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: emojigen <libgtk-4.so> <out-dir>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "emojigen:", err)
		os.Exit(1)
	}
}

func run(lib, out string) error {
	f, err := elf.Open(lib)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	sec := f.Section(".gresource.gtk")
	if sec == nil {
		return errors.New("no .gresource.gtk section")
	}
	data, err := sec.Data()
	if err != nil {
		return err
	}
	entries, err := gvdbEntries(data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil { //nolint:gosec // a source directory
		return err
	}
	n := 0
	for name, value := range entries {
		lang, ok := strings.CutPrefix(name, prefix)
		if !ok || !strings.HasSuffix(lang, ".data") {
			continue
		}
		table, err := resourceBytes(value)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := writeGzip(filepath.Join(out, lang+".gz"), table); err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		return errors.New("no emoji tables in the resource")
	}
	fmt.Printf("wrote %d emoji tables\n", n)
	return nil
}

// gvdbEntries walks a GVDB file's root hash table (gvdb-format.h) into
// full key -> serialized value (a 'v' item's bytes).
func gvdbEntries(b []byte) (map[string][]byte, error) {
	if len(b) < 24 || string(b[:8]) != "GVariant" {
		return nil, errors.New("not a GVDB file")
	}
	le := binary.LittleEndian
	rootStart, rootEnd := int(le.Uint32(b[16:])), int(le.Uint32(b[20:]))
	if rootEnd > len(b) || rootStart+8 > rootEnd {
		return nil, errors.New("bad root pointer")
	}
	root := b[rootStart:rootEnd]
	nBloom := int(le.Uint32(root[0:]) & (1<<27 - 1))
	nBuckets := int(le.Uint32(root[4:]))
	itemsAt := 8 + 4*nBloom + 4*nBuckets
	if itemsAt > len(root) {
		return nil, errors.New("bad hash header")
	}
	type item struct {
		parent uint32
		key    string
		typ    byte
		start  int
		end    int
	}
	var items []item
	for off := itemsAt; off+24 <= len(root); off += 24 {
		it := root[off:]
		keyStart, keySize := int(le.Uint32(it[8:])), int(le.Uint16(it[12:]))
		if keyStart+keySize > len(b) {
			return nil, errors.New("bad key pointer")
		}
		items = append(items, item{
			parent: le.Uint32(it[4:]),
			key:    string(b[keyStart : keyStart+keySize]),
			typ:    it[14],
			start:  int(le.Uint32(it[16:])),
			end:    int(le.Uint32(it[20:])),
		})
	}
	full := func(i int) string {
		var parts []string
		for range items {
			parts = append([]string{items[i].key}, parts...)
			p := items[i].parent
			if p == 0xffffffff || int(p) >= len(items) {
				break
			}
			i = int(p)
		}
		return strings.Join(parts, "")
	}
	out := map[string][]byte{}
	for i, it := range items {
		if it.typ != 'v' || it.end > len(b) || it.start > it.end {
			continue
		}
		out[full(i)] = b[it.start:it.end]
	}
	return out, nil
}

// resourceBytes decodes a GResource entry: a variant holding (uuay) -
// size, flags, data - with flag 1 marking zlib compression.
func resourceBytes(serialized []byte) ([]byte, error) {
	v, err := gvariant.New("v", serialized)
	if err != nil {
		return nil, err
	}
	entry, err := v.Variant()
	if err != nil {
		return nil, err
	}
	if entry.Type != "(uuay)" {
		return nil, fmt.Errorf("entry type %s, want (uuay)", entry.Type)
	}
	fields, err := entry.Fields()
	if err != nil {
		return nil, err
	}
	flags, err := fields[1].Uint32()
	if err != nil {
		return nil, err
	}
	data := fields[2].Data
	if flags&1 == 0 {
		// Uncompressed data carries a trailing nul GResource adds.
		return bytes.TrimSuffix(data, []byte{0}), nil
	}
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func writeGzip(path string, data []byte) error {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644) //nolint:gosec // a checked-in data file
}
