package repo

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/accur8/locus-go/internal/config"
	"github.com/accur8/locus-go/internal/cpath"
	"github.com/accur8/locus-go/internal/model"
	"github.com/klauspost/compress/zstd"
)

// A local repo holding only tool.zst, behind a multiplexer the way "all"
// fronts every real repo.
func zstFixture(t *testing.T, payload []byte) (*Model, string) {
	t.Helper()
	repoDir := t.TempDir()
	dir := filepath.Join(repoDir, "io", "accur8", "tool", "1.0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool-1.0-linux-amd64.zst"), enc.EncodeAll(payload, nil), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.LocusConfig{
		DataDirectory: t.TempDir(),
		Repos: []config.RepoConfig{
			{Type: "local", Name: "releases", Directory: repoDir, GeneratedChecksums: []string{"all"}},
			{Type: "multiplexer", Name: "all", Repos: []string{"releases"}, GeneratedChecksums: []string{"all"}},
		},
	}
	m, err := NewModel(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m, repoDir
}

func fileOf(t *testing.T, rc model.RepoContent) string {
	t.Helper()
	switch c := rc.(type) {
	case model.CacheFile:
		return c.File
	case model.TempFile:
		return c.File
	case nil:
		t.Fatal("no content")
	default:
		t.Fatalf("unexpected content %T", rc)
	}
	return ""
}

func TestMissFallsBackToZstSiblingAndCaches(t *testing.T) {
	payload := bytes.Repeat([]byte("a8 binary bytes\n"), 4096)
	m, _ := zstFixture(t, payload)
	ctx := context.Background()
	plain := cpath.Parse("io/accur8/tool/1.0/tool-1.0-linux-amd64")

	for _, repoName := range []string{"releases", "all"} {
		repo := m.RepoByName(repoName)
		rc, err := repo.ResolveContent(ctx, plain, true)
		if err != nil {
			t.Fatalf("%s: %v", repoName, err)
		}
		got, err := os.ReadFile(fileOf(t, rc))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("%s: served %d bytes, want the %d decompressed bytes", repoName, len(got), len(payload))
		}
		if _, ok := rc.(model.CacheFile); !ok {
			t.Fatalf("%s: served as %T, want a cache file", repoName, rc)
		}
		// The decompressed file sits in the serving repo's cache, so the
		// second request is a cache hit (same file, no second decompression).
		first := fileOf(t, rc)
		before, _ := os.Stat(first)
		again, err := repo.ResolveContent(ctx, plain, true)
		if err != nil {
			t.Fatal(err)
		}
		if fileOf(t, again) != first {
			t.Fatalf("%s: second request served %s, want the cached %s", repoName, fileOf(t, again), first)
		}
		after, _ := os.Stat(first)
		if !after.ModTime().Equal(before.ModTime()) {
			t.Fatalf("%s: the cached file was rewritten on the second request", repoName)
		}
	}

	// The archive itself is still served, untouched.
	zc, err := m.RepoByName("all").ResolveContent(ctx, plain.AppendExtension("zst"), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zstd.NewReader(bytes.NewReader(mustRead(t, fileOf(t, zc)))); err != nil {
		t.Fatalf("the .zst is no longer a zstd stream: %v", err)
	}

	// A checksum of the plain file is over the DECOMPRESSED bytes.
	cs, err := m.RepoByName("all").ResolveContent(ctx, plain.AppendExtension("sha256"), true)
	if err != nil {
		t.Fatal(err)
	}
	gc, ok := cs.(model.GeneratedContent)
	if !ok {
		t.Fatalf("checksum came back as %T", cs)
	}
	if len(gc.Content) != 64 {
		t.Fatalf("sha256 %q", gc.Content)
	}
}

func TestNoSiblingStaysAMiss(t *testing.T) {
	m, _ := zstFixture(t, []byte("x"))
	ctx := context.Background()
	for _, p := range []string{"io/accur8/tool/1.0/other", "io/accur8/tool/1.0/tool-1.0-linux-amd64.zst.zst", "io/accur8/nope/tool"} {
		rc, err := m.RepoByName("all").ResolveContent(ctx, cpath.Parse(p), true)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if rc != nil {
			t.Fatalf("%s: served %T, want a miss", p, rc)
		}
	}
}

func TestCorruptZstIsAMissNotAnError(t *testing.T) {
	m, repoDir := zstFixture(t, []byte("x"))
	if err := os.WriteFile(filepath.Join(repoDir, "io", "accur8", "tool", "1.0", "broken.zst"), []byte("not zstd"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc, err := m.RepoByName("all").ResolveContent(context.Background(), cpath.Parse("io/accur8/tool/1.0/broken"), true)
	if err != nil {
		t.Fatalf("a corrupt archive must not fail the request: %v", err)
	}
	if rc != nil {
		t.Fatalf("served %T from a corrupt archive", rc)
	}
}

func TestListingShowsThePlainNameBesideTheArchive(t *testing.T) {
	m, _ := zstFixture(t, []byte("x"))
	repo := m.RepoByName("all")
	entries, found, err := repo.Entries(context.Background(), cpath.Parse("io/accur8/tool/1.0/"))
	if err != nil || !found {
		t.Fatalf("entries: found=%v err=%v", found, err)
	}
	extra := zstSiblingGenerator{}.ExtraEntries(entries, repo)
	if len(extra) != 1 || extra[0].Name != "tool-1.0-linux-amd64" || !extra[0].Generated || extra[0].Size != nil {
		t.Fatalf("extra entries %+v", extra)
	}
	// Once the plain file is a real entry it is not added twice.
	entries = append(entries, model.DirectoryEntry{Name: "tool-1.0-linux-amd64", Repo: repo})
	extra = zstSiblingGenerator{}.ExtraEntries(entries, repo)
	if len(extra) != 0 {
		t.Fatalf("plain entry duplicated: %+v", extra)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
