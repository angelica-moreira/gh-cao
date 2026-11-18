package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fuzzArchiveSeed struct {
	names string
	types []byte
	mode  int64
	link  string
	data  []byte
}

var fuzzArchiveSeeds = []fuzzArchiveSeed{
	{"root/demo/cao.json", []byte{tar.TypeReg}, 0o644, "", []byte("{}")},
	{"root/\nroot/demo/\nroot/demo/run.sh", []byte{tar.TypeDir, tar.TypeDir, tar.TypeReg}, 0o4777, "", []byte("#!/bin/sh")},
	{"root/demo/../../escape", []byte{tar.TypeReg}, 0o644, "", []byte("x")},
	{"../escape\n/abs/escape", []byte{tar.TypeReg}, 0o644, "", []byte("x")},
	{"root/demo/link", []byte{tar.TypeSymlink}, 0o777, "../../../escape", nil},
	{"root/demo/hard", []byte{tar.TypeLink}, 0o644, "root/demo/cao.json", nil},
	{"root/demo/a\nother/demo/b", []byte{tar.TypeReg}, 0o644, "", []byte("x")},
	{"root/demo/x\nroot/demo/x", []byte{tar.TypeReg}, 0o644, "", []byte("x")},
	{"root/demo\\..\\x\nroot/demo/c:x", []byte{tar.TypeReg}, 0o644, "", []byte("x")},
	{"root/package.json\nroot/dashboard/a.js\nroot/cao.sh", []byte{tar.TypeReg}, 0o755, "", []byte("x")},
	{"root/.github/workflows/shared/control.md\nroot/unowned", []byte{tar.TypeReg}, 0o644, "", []byte("x")},
	{"root/dashboard/dev", []byte{tar.TypeChar, tar.TypeFifo}, 0o644, "", nil},
	{"../dashboard/escape.js", []byte{tar.TypeReg}, 0o644, "", []byte("x")},
	{"root/../../dashboard/escape.js\nroot/demo/../../../escape", []byte{tar.TypeReg}, 0o644, "", []byte("x")},
}

func addFuzzArchiveSeeds(f *testing.F) {
	for _, seed := range fuzzArchiveSeeds {
		f.Add(seed.names, seed.types, seed.mode, seed.link, seed.data)
	}
}

// fuzzArchive builds a gzip tar with up to four entries; ok is false when tar rejects a header.
func fuzzArchive(names string, types []byte, mode int64, link string, data []byte) (archive []byte, ok bool) {
	if len(types) == 0 {
		types = []byte{tar.TypeReg}
	}
	var buffer bytes.Buffer
	compressed := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(compressed)
	for index, name := range strings.SplitN(names, "\n", 4) {
		header := &tar.Header{Name: name, Typeflag: types[index%len(types)], Mode: mode, Linkname: link}
		if header.Typeflag == tar.TypeReg {
			header.Size = int64(len(data))
		}
		if writer.WriteHeader(header) != nil {
			return nil, false
		}
		if header.Size > 0 {
			if _, err := writer.Write(data); err != nil {
				return nil, false
			}
		}
	}
	if writer.Close() != nil || compressed.Close() != nil {
		return nil, false
	}
	return buffer.Bytes(), true
}

// assertConfinedTree fails when anything other than plain directories and
// non-setuid regular files exists under parent, or anything exists outside allowed.
func assertConfinedTree(t *testing.T, parent, allowed string) {
	t.Helper()
	err := filepath.WalkDir(parent, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			t.Fatalf("extracted non-regular entry %s (%s)", name, info.Mode())
		}
		if info.Mode().IsRegular() {
			if info.Mode().Perm()&^0o755 != 0 || info.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) != 0 {
				t.Fatalf("extracted %s with mode %s", name, info.Mode())
			}
			if relative, err := filepath.Rel(allowed, name); err != nil || !filepath.IsLocal(relative) {
				t.Fatalf("extracted %s outside %s", name, allowed)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func FuzzStageCampaignArchive(f *testing.F) {
	addFuzzArchiveSeeds(f)
	f.Fuzz(func(t *testing.T, names string, types []byte, mode int64, link string, data []byte) {
		archive, ok := fuzzArchive(names, types, mode, link, data)
		if !ok {
			t.Skip()
		}
		parent := t.TempDir()
		staging := filepath.Join(parent, "staging")
		if err := os.Mkdir(staging, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = stageCampaignArchive(context.Background(), bytes.NewReader(archive), staging, "demo")
		assertConfinedTree(t, parent, filepath.Join(staging, "demo"))
	})
}

func FuzzNativeExtractRuntime(f *testing.F) {
	addFuzzArchiveSeeds(f)
	f.Fuzz(func(t *testing.T, names string, types []byte, mode int64, link string, data []byte) {
		archive, ok := fuzzArchive(names, types, mode, link, data)
		if !ok {
			t.Skip()
		}
		filename := filepath.Join(t.TempDir(), "runtime.tgz")
		if err := os.WriteFile(filename, archive, 0o600); err != nil {
			t.Fatal(err)
		}
		parent := t.TempDir()
		destination := filepath.Join(parent, "runtime")
		if err := os.Mkdir(destination, 0o755); err != nil {
			t.Fatal(err)
		}
		root, err := nativeExtractRuntime(context.Background(), filename, destination)
		assertConfinedTree(t, parent, destination)
		if err == nil {
			if relative, relErr := filepath.Rel(destination, root); relErr != nil || !filepath.IsLocal(relative) || strings.ContainsRune(relative, filepath.Separator) {
				t.Fatalf("runtime root %s is not a direct child of %s", root, destination)
			}
		}
	})
}
