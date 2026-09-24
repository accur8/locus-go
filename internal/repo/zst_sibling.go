package repo

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/accur8/locus-go/internal/checksum"
	"github.com/accur8/locus-go/internal/cpath"
	"github.com/accur8/locus-go/internal/model"
	"github.com/klauspost/compress/zstd"
)

// Serving X when only X.zst exists.
//
// Every a8 binary is published compressed, as <name>.zst, and nothing else in
// the repos is a .zst. A request for the plain <name> used to be a 404, so a
// person who needed the binary itself fetched the archive, decompressed it by
// hand and could only verify a checksum of the archive. Now a miss for X falls
// back to X.zst: the archive is resolved through the ordinary chain, streamed
// through zstd into the cache location for X, and served as a cache file from
// then on — with the usual x-checksum headers, computed over the decompressed
// bytes.
//
// This is a FALLBACK after generated, cached and downloaded content have all
// missed, not a content generator. Generators run first for every request, so
// one that claimed every path would probe every upstream for a .zst sibling on
// every ordinary miss. As a fallback it costs nothing until a real miss.
// Directory listings do use a generator (zstSiblingGenerator), so the plain
// name shows next to the archive.

const zstExt = "zst"

// resolveFromZstSibling answers a miss for p from p + ".zst", or returns nil.
func (r *Repo) resolveFromZstSibling(ctx context.Context, p cpath.ContentPath) (model.RepoContent, error) {
	if p.IsDir || p.IsEmpty() {
		return nil, nil
	}
	ext := p.Extension()
	// A .zst has no .zst.zst (this is also what stops the recursion), and a
	// checksum file is generated, never decompressed.
	if strings.EqualFold(ext, zstExt) || checksum.IsChecksumExt(ext) {
		return nil, nil
	}
	log := model.LogFrom(ctx)

	// includeGenerated=false: nothing generates a .zst, and the checksum
	// generator would otherwise be consulted for every miss.
	zc, err := r.ResolveContent(ctx, p.AppendExtension(zstExt), false)
	if err != nil || zc == nil {
		return nil, err
	}
	var archive string
	var serving *Repo
	switch c := zc.(type) {
	case model.CacheFile:
		archive = c.File
		serving, _ = c.R.(*Repo)
	case model.TempFile:
		archive = c.File
		serving, _ = c.R.(*Repo)
	default:
		return nil, nil
	}
	if serving == nil {
		serving = r
	}

	if !r.m.isCachable(p) {
		tmp, err := r.m.newTempFile()
		if err != nil {
			return nil, err
		}
		if err := decompressZst(archive, tmp); err != nil {
			log.Warn(fmt.Sprintf("repo %s: %s exists but did not decompress; serving nothing for %s", serving.Name(), p.AppendExtension(zstExt), p), err)
			return nil, nil
		}
		return model.TempFile{File: tmp, R: serving}, nil
	}

	// The decompressed file lives in the SERVING repo's cache, where the
	// ordinary cache lookup finds it on the next request — and where this
	// fallback finds it too, for a backend (local) whose cache lookup reads
	// only its own directory.
	cl := serving.cacheLocation(p)
	if cached, err := cl.toCacheFile(ctx); err != nil || cached != nil {
		return cached, err
	}
	if err := decompressZst(archive, cl.file); err != nil {
		log.Warn(fmt.Sprintf("repo %s: %s exists but did not decompress; serving nothing for %s", serving.Name(), p.AppendExtension(zstExt), p), err)
		return nil, nil
	}
	log.Info(fmt.Sprintf("repo %s: decompressed %s into the cache as %s", serving.Name(), p.AppendExtension(zstExt), p))
	return cl.toCacheFile(ctx)
}

// decompressZst streams src (a zstd frame) into dst, writing a temp file
// beside dst and renaming into place so a concurrent request never sees a
// partial file.
func decompressZst(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	dec, err := zstd.NewReader(in)
	if err != nil {
		return err
	}
	defer dec.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".zst-*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	if _, err := io.Copy(out, dec); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// zstSiblingGenerator contributes ONLY listing entries: for every X.zst in a
// directory it lists X beside it, so a person browsing the repo sees the
// plain file the fallback above will serve. It generates no content itself.
type zstSiblingGenerator struct{}

func (zstSiblingGenerator) CanGenerateFor(cpath.ContentPath) bool { return false }

func (zstSiblingGenerator) Generate(context.Context, string, cpath.ContentPath, model.ResolvedRepo) (model.RepoContent, error) {
	return nil, nil
}

func (zstSiblingGenerator) ExtraEntries(entries []model.DirectoryEntry, _ model.ResolvedRepo) []model.DirectoryEntry {
	existing := map[string]bool{}
	for _, e := range entries {
		existing[e.Name] = true
	}
	var out []model.DirectoryEntry
	for _, e := range entries {
		if e.IsDirectory || !strings.HasSuffix(strings.ToLower(e.Name), "."+zstExt) {
			continue
		}
		plain := e.Name[:len(e.Name)-len("."+zstExt)]
		if plain == "" || existing[plain] {
			continue
		}
		out = append(out, model.DirectoryEntry{
			Name:         plain,
			Repo:         e.Repo,
			LastModified: e.LastModified,
			Size:         nil, // unknown until decompressed
			Generated:    true,
		})
	}
	return out
}
