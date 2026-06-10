package repo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/accur8/locus-go/internal/checksum"
	"github.com/accur8/locus-go/internal/cpath"
	"github.com/accur8/locus-go/internal/javatime"
	"github.com/accur8/locus-go/internal/model"
	"github.com/accur8/locus-go/internal/upstream"
)

// ============================ HTTP backend ============================

type httpBackend struct{ r *Repo }

func (b *httpBackend) resolvedAuth() *upstream.BasicAuth {
	u := b.r.cfg.URL
	if u.HasUser {
		return &upstream.BasicAuth{Username: u.User, Password: u.Password}
	}
	return nil
}

func (b *httpBackend) entries0(ctx context.Context, p cpath.ContentPath) ([]model.DirectoryEntry, bool, error) {
	target := b.r.cfg.URL.Join(p.FullPath())
	resp, err := b.r.m.HTTP.Get(ctx, target, nil, true, b.r.m.newTempFile)
	if err != nil {
		return nil, false, err
	}
	if resp.Status != 200 || resp.BodyFile == "" {
		return nil, false, nil
	}
	body, err := upstream.ReadFileString(resp.BodyFile)
	if err != nil {
		return nil, false, err
	}
	entries, err := upstream.ParseMavenIndex(target, body, b.r)
	if err != nil {
		return nil, false, err
	}
	return entries, true, nil
}

func (b *httpBackend) singleDownload0(ctx context.Context, p cpath.ContentPath) (model.DownloadResult, error) {
	target := b.r.cfg.URL.JoinParts(p.Parts)
	resp, err := b.r.m.HTTP.Get(ctx, target, b.resolvedAuth(), false, b.r.m.newTempFile)
	if err != nil {
		return nil, err
	}
	model.LogFrom(ctx).Tracef("%s.singleDownload0 GET of %s returned %d", b.r.cfg.Name, target.String(), resp.Status)

	switch resp.Status {
	case 200:
		if p.IsDir {
			return model.DownloadAsRepoContent{Content: model.GeneratedFile{File: resp.BodyFile, ContentType: model.CTHtml, R: b.r}}, nil
		}
		if resp.BodyFile == "" {
			return nil, fmt.Errorf("got no response body on a 200 this should not happen")
		}
		return model.DownloadSuccess{R: b.r, File: resp.BodyFile}, nil
	case 404, 403:
		return nil, nil
	case 302:
		location := resp.Header("Location")
		prefix, hasPrefix := b.r.cfg.URL.Path()
		if hasPrefix {
			if strings.HasPrefix(location, prefix) {
				resolved := location[len(prefix):]
				return model.DownloadAsRepoContent{Content: model.Redirect{Path: cpath.Parse(resolved), R: b.r}}, nil
			}
			model.LogFrom(ctx).Warn(fmt.Sprintf("redirect doesn't match remoteLocationPrefix -- %s -- %s", location, prefix), nil)
			return nil, nil
		}
		return model.DownloadAsRepoContent{Content: model.Redirect{Path: cpath.Parse(location), R: b.r}}, nil
	default:
		model.LogFrom(ctx).Error(fmt.Sprintf("unsupported status %d on GET %s", resp.Status, target.String()), nil)
		return nil, nil
	}
}

func (b *httpBackend) cachedContent(ctx context.Context, p cpath.ContentPath) (model.RepoContent, error) {
	return b.r.defaultCachedContent(ctx, p)
}
func (b *httpBackend) put(ctx context.Context, p cpath.ContentPath, srcFile string) (model.PutResult, error) {
	return model.PutNotAllowed, nil
}
func (b *httpBackend) clearCache(ctx context.Context, p cpath.ContentPath) ([]model.ClearedEntry, error) {
	return b.r.defaultClearCache(ctx, p)
}

// ============================ S3 backend ============================

type s3Backend struct{ r *Repo }

var errNoS3 = fmt.Errorf("s3 config is required but none was provided")

func (b *s3Backend) bucket() string { return b.r.cfg.URL.Host }

func (b *s3Backend) keyPrefix() cpath.ContentPath {
	if path, ok := b.r.cfg.URL.Path(); ok {
		return cpath.Parse(path).AsFile()
	}
	return cpath.Empty
}

func (b *s3Backend) entries0(ctx context.Context, p cpath.ContentPath) ([]model.DirectoryEntry, bool, error) {
	if b.r.m.S3 == nil {
		return nil, false, errNoS3
	}
	baseURL := b.r.cfg.URL.JoinParts(p.Parts)
	keyDir := b.keyPrefix().Append(p).AsFile()
	prefix := keyDir.AsDirectory().FullPath()
	list, err := b.r.m.S3.List(ctx, b.bucket(), prefix)
	if err != nil {
		// A 404 (e.g. NoSuchBucket) is treated as an absent directory, matching
		// ResolvedS3Repo.entries0; this lets the multiplexer still render other
		// repos instead of failing the whole listing.
		if upstream.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	entries := make([]model.DirectoryEntry, 0, len(list))
	for _, e := range list {
		if e.IsDir {
			entries = append(entries, model.DirectoryEntry{Name: e.DirName, IsDirectory: true, Repo: b.r})
			continue
		}
		name := cpath.Parse(e.Obj.Key).Last()
		url := baseURL.Join(name)
		lm := javatime.FromEpochMillis(e.Obj.LastModifiedMillis)
		size := e.Obj.Size
		entries = append(entries, model.DirectoryEntry{
			Name: name, IsDirectory: false, Repo: b.r,
			LastModified: &lm, Size: &size, DirectURL: &url,
		})
	}
	return entries, true, nil
}

func (b *s3Backend) singleDownload0(ctx context.Context, p cpath.ContentPath) (model.DownloadResult, error) {
	if b.r.m.S3 == nil {
		return nil, errNoS3
	}
	key := b.keyPrefix().Append(p).AsFile().FullPath()
	tmp, err := b.r.m.newTempFile()
	if err != nil {
		return nil, err
	}
	model.LogFrom(ctx).Tracef("%s.singleDownload0 S3 GET %s %s", b.r.cfg.Name, b.bucket(), key)
	found, err := b.r.m.S3.GetObject(ctx, b.bucket(), key, tmp)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return model.DownloadSuccess{R: b.r, File: tmp}, nil
}

func (b *s3Backend) cachedContent(ctx context.Context, p cpath.ContentPath) (model.RepoContent, error) {
	return b.r.defaultCachedContent(ctx, p)
}

func (b *s3Backend) put(ctx context.Context, p cpath.ContentPath, srcFile string) (model.PutResult, error) {
	if b.r.m.S3 == nil {
		return 0, errNoS3
	}
	key := b.keyPrefix().Append(p).FullPath()
	exists, err := b.r.m.S3.HeadObject(ctx, b.bucket(), key)
	if err != nil {
		return 0, err
	}
	if exists {
		return model.PutAlreadyExists, nil
	}
	md5b, err := checksum.Md5.DigestFile(srcFile)
	if err != nil {
		return 0, err
	}
	if err := b.r.m.S3.PutObject(ctx, b.bucket(), key, srcFile, checksum.Base64String(md5b)); err != nil {
		return 0, err
	}
	return model.PutSuccess, nil
}

func (b *s3Backend) clearCache(ctx context.Context, p cpath.ContentPath) ([]model.ClearedEntry, error) {
	return b.r.defaultClearCache(ctx, p)
}

// ============================ Local backend ============================

type localBackend struct{ r *Repo }

func (b *localBackend) rootDir() string { return b.r.cfg.Directory }

func (b *localBackend) cachedContent(ctx context.Context, p cpath.ContentPath) (model.RepoContent, error) {
	file := filepath.Join(b.rootDir(), filepath.FromSlash(p.FullPath()))
	fi, err := os.Stat(file)
	if err != nil || fi.IsDir() {
		return nil, nil
	}
	return model.CacheFile{File: file, R: b.r}, nil
}

func (b *localBackend) singleDownload0(ctx context.Context, p cpath.ContentPath) (model.DownloadResult, error) {
	return nil, nil
}

// entries0 intentionally diverges from ResolvedLocalRepo.entries0, whose
// `exists.toOption(None).getOrElse(Some(entries))` is inverted/broken (it
// returns None when the directory exists and Some(empty) when it does not, so it
// could never list a local repo's contents). We return the actual entries with
// found=true when the directory exists, and found=false when it is missing.
func (b *localBackend) entries0(ctx context.Context, p cpath.ContentPath) ([]model.DirectoryEntry, bool, error) {
	dir := filepath.Join(b.rootDir(), filepath.FromSlash(p.String()))
	des, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	entries := make([]model.DirectoryEntry, 0, len(des))
	for _, de := range des {
		if de.IsDir() {
			entries = append(entries, model.DirectoryEntry{Name: de.Name(), IsDirectory: true, Repo: b.r})
			continue
		}
		fi, err := de.Info()
		if err != nil {
			return nil, false, err
		}
		lm := javatime.FromEpochMillis(fi.ModTime().UnixMilli())
		size := fi.Size()
		entries = append(entries, model.DirectoryEntry{
			Name: de.Name(), IsDirectory: false, Repo: b.r,
			LastModified: &lm, Size: &size,
		})
	}
	return entries, true, nil
}

func (b *localBackend) put(ctx context.Context, p cpath.ContentPath, srcFile string) (model.PutResult, error) {
	dst := filepath.Join(b.rootDir(), filepath.FromSlash(p.String()))
	if _, err := os.Stat(dst); err == nil {
		return model.PutAlreadyExists, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	if err := copyFile(srcFile, dst); err != nil {
		return 0, err
	}
	return model.PutSuccess, nil
}

func (b *localBackend) clearCache(ctx context.Context, p cpath.ContentPath) ([]model.ClearedEntry, error) {
	return b.r.defaultClearCache(ctx, p)
}

// ============================ Multiplexer backend ============================

type muxBackend struct{ r *Repo }

func (b *muxBackend) delegates() []*Repo {
	out := make([]*Repo, 0, len(b.r.cfg.Repos))
	for _, name := range b.r.cfg.Repos {
		if d := b.r.m.RepoByName(name); d != nil {
			out = append(out, d)
		}
	}
	return out
}

func (b *muxBackend) repoForWrites() *Repo {
	if b.r.cfg.HasRepoForWrites {
		return b.r.m.RepoByName(b.r.cfg.RepoForWrites)
	}
	return nil
}

func (b *muxBackend) cachedContent(ctx context.Context, p cpath.ContentPath) (model.RepoContent, error) {
	if !b.r.m.isCachable(p) {
		return nil, nil
	}
	results, err := runParallel(mapDelegates(b.delegates(), func(d *Repo) func() (any, error) {
		return func() (any, error) { return d.be.cachedContent(ctx, p) }
	}))
	if err != nil {
		return nil, err
	}
	for _, res := range results {
		if rc, _ := res.(model.RepoContent); rc != nil {
			return rc, nil
		}
	}
	return nil, nil
}

func (b *muxBackend) singleDownload0(ctx context.Context, p cpath.ContentPath) (model.DownloadResult, error) {
	results, err := runParallel(mapDelegates(b.delegates(), func(d *Repo) func() (any, error) {
		return func() (any, error) { return d.be.singleDownload0(ctx, p) }
	}))
	if err != nil {
		return nil, err
	}
	for _, res := range results {
		if dr, _ := res.(model.DownloadResult); dr != nil {
			return dr, nil
		}
	}
	return nil, nil
}

func (b *muxBackend) entries0(ctx context.Context, p cpath.ContentPath) ([]model.DirectoryEntry, bool, error) {
	delegates := b.delegates()
	type res struct {
		entries []model.DirectoryEntry
		found   bool
		err     error
	}
	results := make([]res, len(delegates))
	var wg sync.WaitGroup
	for i, d := range delegates {
		wg.Add(1)
		go func(i int, d *Repo) {
			defer wg.Done()
			e, found, err := d.Entries(ctx, p)
			results[i] = res{e, found, err}
		}(i, d)
	}
	wg.Wait()

	var merged []model.DirectoryEntry
	anyFound := false
	for _, r := range results {
		if r.err != nil {
			return nil, false, r.err
		}
		if r.found {
			anyFound = true
			merged = append(merged, r.entries...)
		}
	}
	return merged, anyFound, nil
}

func (b *muxBackend) put(ctx context.Context, p cpath.ContentPath, srcFile string) (model.PutResult, error) {
	if rfw := b.repoForWrites(); rfw != nil {
		return rfw.Put(ctx, p, srcFile)
	}
	return model.PutNotAllowed, nil
}

func (b *muxBackend) clearCache(ctx context.Context, p cpath.ContentPath) ([]model.ClearedEntry, error) {
	out, err := b.r.defaultClearCache(ctx, p)
	if err != nil {
		return nil, err
	}
	for _, d := range b.delegates() {
		dc, err := d.ClearCache(ctx, p)
		if err != nil {
			return nil, err
		}
		out = append(out, dc...)
	}
	return out, nil
}

// ---- parallel helpers ----

func mapDelegates(delegates []*Repo, fn func(*Repo) func() (any, error)) []func() (any, error) {
	out := make([]func() (any, error), len(delegates))
	for i, d := range delegates {
		out[i] = fn(d)
	}
	return out
}

// runParallel runs tasks concurrently and returns their results in order. The
// first non-nil error (in task order) is returned (mirrors sequencePar fail).
func runParallel(tasks []func() (any, error)) ([]any, error) {
	results := make([]any, len(tasks))
	errs := make([]error, len(tasks))
	var wg sync.WaitGroup
	for i, t := range tasks {
		wg.Add(1)
		go func(i int, t func() (any, error)) {
			defer wg.Done()
			results[i], errs[i] = t()
		}(i, t)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	return results, nil
}
