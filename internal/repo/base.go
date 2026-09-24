package repo

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/accur8/locus-go/internal/checksum"
	"github.com/accur8/locus-go/internal/config"
	"github.com/accur8/locus-go/internal/cpath"
	"github.com/accur8/locus-go/internal/model"
)

const standardRetryCount = 1

// backend provides the per-repo-type behaviour the shared logic delegates to
// (mirrors the abstract / overridable members of the ResolvedRepo trait).
type backend interface {
	// entries0 lists entries; found=false means the directory does not exist.
	entries0(ctx context.Context, p cpath.ContentPath) ([]model.DirectoryEntry, bool, error)
	// singleDownload0 attempts one download; a nil result means "not found".
	singleDownload0(ctx context.Context, p cpath.ContentPath) (model.DownloadResult, error)
	// cachedContent returns a *CacheFile or nil.
	cachedContent(ctx context.Context, p cpath.ContentPath) (model.RepoContent, error)
	// put stores srcFile at p.
	put(ctx context.Context, p cpath.ContentPath, srcFile string) (model.PutResult, error)
	// clearCache removes cached files at/under p.
	clearCache(ctx context.Context, p cpath.ContentPath) ([]model.ClearedEntry, error)
}

// Repo is the concrete ResolvedRepo, composing shared logic with a backend.
type Repo struct {
	cfg          config.RepoConfig
	m            *Model
	be           backend
	genChecksums []checksum.Kind
	gens         []model.ContentGenerator
}

func newRepo(m *Model, rc config.RepoConfig) (*Repo, error) {
	r := &Repo{
		cfg:          rc,
		m:            m,
		genChecksums: checksumHandlersFor(rc.GeneratedChecksums),
		gens:         defaultGenerators,
	}
	switch rc.Type {
	case "multiplexer":
		r.be = &muxBackend{r: r}
	case "url":
		switch strings.ToLower(rc.URL.Scheme) {
		case "http", "https":
			r.be = &httpBackend{r: r}
		case "s3":
			r.be = &s3Backend{r: r}
		default:
			return nil, fmt.Errorf("repo %q: unsupported url scheme %q", rc.Name, rc.URL.Scheme)
		}
	case "local":
		r.be = &localBackend{r: r}
	default:
		return nil, fmt.Errorf("repo %q: unknown type %q", rc.Name, rc.Type)
	}
	return r, nil
}

// ---- model.ResolvedRepo ----

func (r *Repo) Name() string                                { return r.cfg.Name }
func (r *Repo) GeneratedChecksumHandlers() []checksum.Kind  { return r.genChecksums }
func (r *Repo) ContentGenerators() []model.ContentGenerator { return r.gens }

func (r *Repo) Entries(ctx context.Context, p cpath.ContentPath) ([]model.DirectoryEntry, bool, error) {
	return r.be.entries0(ctx, p)
}

func (r *Repo) Put(ctx context.Context, p cpath.ContentPath, srcFile string) (model.PutResult, error) {
	return r.be.put(ctx, p, srcFile)
}

func (r *Repo) ClearCache(ctx context.Context, p cpath.ContentPath) ([]model.ClearedEntry, error) {
	return r.be.clearCache(ctx, p)
}

// ResolveContent mirrors ResolvedRepo.resolveContent: generated (optional) then
// cached then download, first non-nil wins; the result is written to cache.
// One addition over the Scala: a miss then falls back to a .zst sibling.
func (r *Repo) ResolveContent(ctx context.Context, p cpath.ContentPath, includeGenerated bool) (model.RepoContent, error) {
	var result model.RepoContent
	var err error

	if includeGenerated {
		if result, err = r.resolveGeneratedContent(ctx, p); err != nil {
			return nil, err
		}
	}
	if result == nil {
		if result, err = r.be.cachedContent(ctx, p); err != nil {
			return nil, err
		}
	}
	if result == nil {
		if result, err = r.downloadContent(ctx, p); err != nil {
			return nil, err
		}
	}
	if result == nil {
		// Last: X from X.zst (zst_sibling.go). After every ordinary source has
		// missed, so an existing file never pays for the probe.
		if result, err = r.resolveFromZstSibling(ctx, p); err != nil {
			return nil, err
		}
	}
	if werr := r.writeToCache(ctx, p, result); werr != nil {
		model.LogFrom(ctx).Warn("writeToCache failed", werr)
	}
	return result, nil
}

func (r *Repo) resolveGeneratedContent(ctx context.Context, p cpath.ContentPath) (model.RepoContent, error) {
	for _, g := range r.gens {
		if g.CanGenerateFor(p) {
			return g.Generate(ctx, "/repos/"+r.cfg.Name, p, r)
		}
	}
	return nil, nil
}

// downloadContent mirrors downloadContentWithRetries.
func (r *Repo) downloadContent(ctx context.Context, p cpath.ContentPath) (model.RepoContent, error) {
	log := model.LogFrom(ctx)
	retriesLeft := standardRetryCount
	for {
		dr, err := r.be.singleDownload0(ctx, p)
		if err != nil {
			log.Warn(fmt.Sprintf("repo %s error downloading %s, %d retries left", r.cfg.Name, p, retriesLeft), err)
			if retriesLeft >= 1 {
				retriesLeft--
				continue
			}
			log.Error(fmt.Sprintf("repo %s unable to download %s, no retries left", r.cfg.Name, p), nil)
			return nil, nil
		}
		switch d := dr.(type) {
		case nil:
			return nil, nil
		case model.DownloadSuccess:
			checksums, verr := validateChecksums(ctx, d.R, d.File, p)
			if verr != nil {
				log.Warn(fmt.Sprintf("repo %s failed checksums for %s, %d retries left", r.cfg.Name, p, retriesLeft), verr)
				if retriesLeft >= 1 {
					retriesLeft--
					continue
				}
				return nil, nil
			}
			return model.TempFile{File: d.File, R: d.R, Checksums: checksums}, nil
		case model.DownloadAsRepoContent:
			return d.Content, nil
		default:
			return nil, nil
		}
	}
}

// validateChecksums mirrors ResolvedRepo.validateChecksums: validate the file
// against the repo's published md5/sha1 (not sha256). No published checksum =>
// pass. Mirrors using the *serving* repo to resolve checksum files.
func validateChecksums(ctx context.Context, repo model.ResolvedRepo, file string, p cpath.ContentPath) ([]model.Checksum, error) {
	if checksum.IsChecksumExt(p.Extension()) {
		return nil, nil
	}
	var valids []model.Checksum
	var invalids []string
	for _, k := range checksum.Validators {
		checksumPath := p.AppendExtension(k.Ext)
		csFile, err := resolveContentAsFile(ctx, repo, checksumPath)
		if err != nil {
			return nil, err
		}
		if csFile == "" {
			continue
		}
		raw, err := os.ReadFile(csFile)
		if err != nil {
			return nil, err
		}
		digest, err := k.DigestFile(file)
		if err != nil {
			return nil, err
		}
		expected := checksum.Scrub(string(raw))
		actual := checksum.Scrub(checksum.HexString(digest))
		if actual == expected {
			valids = append(valids, model.Checksum{Ext: k.Ext, Value: expected})
		} else {
			invalids = append(invalids, fmt.Sprintf("%s mismatch actual != expected -- %s != %s", k.Ext, actual, expected))
		}
	}
	if len(invalids) > 0 {
		return nil, fmt.Errorf("repo %s checksum failed for %s -- %s", repo.Name(), p, strings.Join(invalids, "  --  "))
	}
	return valids, nil
}

func resolveContentAsFile(ctx context.Context, repo model.ResolvedRepo, p cpath.ContentPath) (string, error) {
	rc, err := repo.ResolveContent(ctx, p, false)
	if err != nil {
		return "", err
	}
	switch c := rc.(type) {
	case model.TempFile:
		return c.File, nil
	case model.CacheFile:
		return c.File, nil
	default:
		return "", nil
	}
}

// writeToCache mirrors ResolvedRepo.writeToCache.
func (r *Repo) writeToCache(ctx context.Context, p cpath.ContentPath, content model.RepoContent) error {
	if content == nil {
		return nil
	}
	if checksum.IsChecksumExt(p.Extension()) {
		return nil
	}
	tf, ok := content.(model.TempFile)
	if !ok || !r.m.isCachable(p) {
		return nil
	}
	servingRepo, ok := tf.R.(*Repo)
	if !ok {
		return nil
	}
	cl := servingRepo.cacheLocation(p)
	if err := cl.copyFrom(ctx, tf.File); err != nil {
		return err
	}
	for _, cs := range tf.Checksums {
		csLoc := servingRepo.cacheLocation(p.AppendExtension(cs.Ext))
		if err := csLoc.write(ctx, cs.Value); err != nil {
			return err
		}
	}
	return nil
}

// ---- cache helpers ----

func (r *Repo) cacheRootDir() string {
	return filepath.Join(r.m.cacheRoot(), strings.ToLower(r.cfg.Name))
}

type cacheLocation struct {
	p    cpath.ContentPath
	file string
	repo *Repo
}

func (r *Repo) cacheLocation(p cpath.ContentPath) cacheLocation {
	parts := append([]string{r.cacheRootDir()}, p.Parts...)
	return cacheLocation{p: p, file: filepath.Join(parts...), repo: r}
}

// defaultCachedContent mirrors ResolvedRepo.cachedContent for non-mux/non-local repos.
func (r *Repo) defaultCachedContent(ctx context.Context, p cpath.ContentPath) (model.RepoContent, error) {
	if !r.m.isCachable(p) {
		return nil, nil
	}
	return r.cacheLocation(p).toCacheFile(ctx)
}

func (cl cacheLocation) toCacheFile(ctx context.Context) (model.RepoContent, error) {
	fi, err := os.Stat(cl.file)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if fi.IsDir() {
		return nil, nil
	}
	if fi.Size() == 0 {
		model.LogFrom(ctx).Warn(fmt.Sprintf("deleting 0 byte cache file %s", cl.file), nil)
		_ = os.Remove(cl.file)
		return nil, nil
	}
	return model.CacheFile{File: cl.file, R: cl.repo}, nil
}

func (cl cacheLocation) write(ctx context.Context, content string) error {
	if len(content) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(cl.file), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(cl.file, []byte(content), 0o644); err != nil {
		return err
	}
	model.LogFrom(ctx).Trace("wrote " + cl.file)
	return nil
}

func (cl cacheLocation) copyFrom(ctx context.Context, src string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if fi.Size() == 0 {
		model.LogFrom(ctx).Warn(fmt.Sprintf("copyFrom zero size file %s, no copy performed", src), nil)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(cl.file), 0o755); err != nil {
		return err
	}
	return copyFile(src, cl.file)
}

// defaultClearCache mirrors ResolvedRepo.clearCache.
func (r *Repo) defaultClearCache(ctx context.Context, p cpath.ContentPath) ([]model.ClearedEntry, error) {
	target := r.cacheLocation(p).file
	fi, statErr := os.Stat(target)
	isDir := statErr == nil && fi.IsDir()

	var dir string
	var filter func(name string) bool
	if isDir {
		dir = target
		filter = func(string) bool { return true }
	} else {
		dir = filepath.Dir(target)
		prefix := p.Last()
		filter = func(name string) bool { return strings.HasPrefix(name, prefix) }
	}

	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []model.ClearedEntry
	for _, de := range dirEntries {
		if !filter(de.Name()) {
			continue
		}
		abs := filepath.Join(dir, de.Name())
		if err := os.RemoveAll(abs); err != nil {
			return nil, err
		}
		out = append(out, model.ClearedEntry{Repo: r, Path: abs})
	}
	return out, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	// Use a unique temp file so concurrent cache writes for the same artifact do
	// not share (and corrupt) one intermediate; rename is atomic on the same fs.
	out, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	if _, err := io.Copy(out, in); err != nil {
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
