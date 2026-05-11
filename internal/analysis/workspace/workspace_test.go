package workspace

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type bytesArchiver struct{ data []byte }

func (b *bytesArchiver) FetchArchive(_ context.Context, _, _ string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.data)), nil
}

func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// Single root dir wrapper to mirror GitHub/GitLab behaviour.
	if err := tw.WriteHeader(&tar.Header{Name: "wrapper/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	for path, content := range files {
		full := "wrapper/" + path
		if err := tw.WriteHeader(&tar.Header{
			Name: full, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestOpenExtractsAndFlattens(t *testing.T) {
	tarData := makeTarGz(t, map[string]string{
		"main.go":       "package main\n",
		"sub/foo.txt":   "hello\n",
	})
	ws, err := Open(context.Background(), &bytesArchiver{data: tarData}, "o/r", "abc")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	if got, _ := os.ReadFile(filepath.Join(ws.Dir, "main.go")); string(got) != "package main\n" {
		t.Errorf("main.go contents: %q", string(got))
	}
	if got, _ := os.ReadFile(filepath.Join(ws.Dir, "sub", "foo.txt")); string(got) != "hello\n" {
		t.Errorf("sub/foo.txt contents: %q", string(got))
	}
	// Wrapper should be flattened away.
	if _, err := os.Stat(filepath.Join(ws.Dir, "wrapper")); !os.IsNotExist(err) {
		t.Errorf("wrapper directory should have been flattened, got err=%v", err)
	}
}

func TestExtractRejectsPathTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	bad := "../../../etc/passwd"
	_ = tw.WriteHeader(&tar.Header{Name: bad, Mode: 0o644, Size: 4, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("oops"))
	_ = tw.Close()
	_ = gz.Close()

	dir := t.TempDir()
	if err := extractTarGz(&buf, dir); err != nil {
		t.Fatal(err)
	}
	// File should NOT have been written outside dir.
	parent := filepath.Dir(filepath.Dir(filepath.Dir(dir)))
	if _, err := os.Stat(filepath.Join(parent, "etc", "passwd")); err == nil {
		t.Fatal("path-traversal succeeded")
	}
}

func TestCloseNilSafe(t *testing.T) {
	var ws *Workspace
	if err := ws.Close(); err != nil {
		t.Errorf("nil close: %v", err)
	}
}
