// Package repo implements the repository resolution core and content generators,
// mirroring a8.locus.ResolvedModel / ResolvedRepo and the *Generator objects.
package repo

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/accur8/locus-go/internal/checksum"
	"github.com/accur8/locus-go/internal/config"
	"github.com/accur8/locus-go/internal/cpath"
	"github.com/accur8/locus-go/internal/model"
	"github.com/accur8/locus-go/internal/upstream"
)

// neverCache mirrors ResolvedModel.neverCacheSet.
var neverCache = map[string]bool{"index.html": true, "maven-metadata.xml": true}

// Model mirrors a8.locus.ResolvedModel: it owns the config, the shared clients,
// and the set of resolved repos.
type Model struct {
	Config *config.LocusConfig
	HTTP   *upstream.HTTPClient
	S3     *upstream.S3Client

	dataDir string
	repos   []*Repo
	byName  map[string]*Repo
}

// NewModel builds the Model and all resolved repos from config.
func NewModel(cfg *config.LocusConfig, http *upstream.HTTPClient, s3 *upstream.S3Client) (*Model, error) {
	m := &Model{
		Config:  cfg,
		HTTP:    http,
		S3:      s3,
		dataDir: cfg.DataDirectory,
		byName:  map[string]*Repo{},
	}
	for _, rc := range cfg.Repos {
		r, err := newRepo(m, rc)
		if err != nil {
			return nil, err
		}
		m.repos = append(m.repos, r)
		m.byName[strings.ToLower(r.cfg.Name)] = r
	}
	return m, nil
}

// ResolvedRepos returns the repos in config order (resolvedProxyPaths).
func (m *Model) ResolvedRepos() []*Repo { return m.repos }

// RepoByName looks up a repo by (case-insensitive) name; nil if absent.
func (m *Model) RepoByName(name string) *Repo { return m.byName[strings.ToLower(name)] }

// isCachable mirrors ResolvedModel.isCachable.
func (m *Model) isCachable(p cpath.ContentPath) bool {
	last := p.Last()
	if neverCache[strings.ToLower(last)] {
		return false
	}
	return !m.Config.IsNoCacheFile(last)
}

func (m *Model) cacheRoot() string { return filepath.Join(m.dataDir, "cache") }
func (m *Model) tempRoot() string  { return filepath.Join(m.dataDir, "temp") }

// newTempFile creates a unique temp file path (parent dirs created), mirroring
// ResolvedModel.tempFile (data/temp/YYYY/MM/DD/<uuid>).
func (m *Model) newTempFile() (string, error) {
	now := time.Now()
	sub := fmt.Sprintf("%d/%02d/%02d", now.Year(), int(now.Month()), now.Day())
	dir := filepath.Join(m.tempRoot(), sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, randomID()), nil
}

// WithWorkDir creates a unique temp directory, runs fn, then removes it
// (mirrors ResolvedModel.withWorkDirectory).
func (m *Model) WithWorkDir(fn func(dir string) error) error {
	now := time.Now()
	sub := fmt.Sprintf("%d/%02d/%02d/%s", now.Year(), int(now.Month()), now.Day(), randomID())
	dir := filepath.Join(m.tempRoot(), sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	return fn(dir)
}

func randomID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)[:20]
}

// checksumHandlersFor resolves a repo's generatedChecksums config into kinds
// (mirrors ResolvedRepo.generatedChecksumHandlers).
func checksumHandlersFor(names []string) []checksum.Kind {
	var out []checksum.Kind
	seen := map[string]bool{}
	add := func(k checksum.Kind) {
		if !seen[k.ExtensionLc()] {
			seen[k.ExtensionLc()] = true
			out = append(out, k)
		}
	}
	for _, name := range names {
		lc := strings.ToLower(name)
		if lc == "all" {
			for _, k := range checksum.All {
				add(k)
			}
			continue
		}
		found := false
		for _, k := range checksum.All {
			if k.ExtensionLc() == lc {
				add(k)
				found = true
				break
			}
		}
		_ = found // unknown names are silently skipped (Scala logs a warning)
	}
	return out
}

// defaultGenerators mirrors ResolvedRepo.defaultContentGenerators order:
// [ChecksumGenerator, GenerateMavenMetadata, GenerateIndexDotHtml], plus the
// listing-only zstSiblingGenerator (it claims no path, so its position is
// irrelevant to content resolution).
var defaultGenerators = []model.ContentGenerator{
	checksumGenerator{},
	mavenMetadataGenerator{},
	indexHTMLGenerator{},
	zstSiblingGenerator{},
}
