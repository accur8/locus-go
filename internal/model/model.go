// Package model holds the shared domain types for repository resolution,
// mirroring a8.locus.ResolvedModel / ResolvedRepo. It depends only on leaf
// packages so both the upstream clients and the repo implementations can import
// it without cycles.
package model

import (
	"context"

	"github.com/accur8/locus-go/internal/checksum"
	"github.com/accur8/locus-go/internal/cpath"
	"github.com/accur8/locus-go/internal/javatime"
	"github.com/accur8/locus-go/internal/uri"
)

// Checksum is an (extension, value) pair (ChecksumHandler.Checksum).
type Checksum struct {
	Ext   string
	Value string
}

// ContentType is a media type string; "" means "no explicit content type".
type ContentType = string

const (
	CTHtml = "text/html"
	CTXml  = "text/xml"
	CTJson = "application/json"
	CTJar  = "application/java-archive"
)

// DirectoryEntry mirrors ResolvedModel.DirectoryEntry.
type DirectoryEntry struct {
	Name         string
	IsDirectory  bool
	Repo         ResolvedRepo
	LastModified *javatime.DateTime // nil if unknown
	Size         *int64             // nil if unknown
	Generated    bool
	DirectURL    *uri.Uri // nil if none
}

// PutResult mirrors ResolvedModel.PutResult.
type PutResult int

const (
	PutSuccess PutResult = iota
	PutNotAllowed
	PutAlreadyExists
)

// ClearedEntry is one cache file removed by clearCache.
type ClearedEntry struct {
	Repo ResolvedRepo
	Path string
}

// ---- RepoContent (sealed) ----

// RepoContent is resolved content from a repo (ResolvedModel.RepoContent).
type RepoContent interface{ ContentRepo() ResolvedRepo }

// TempFile is freshly downloaded content in a temp file, with validated checksums.
type TempFile struct {
	File      string
	R         ResolvedRepo
	Checksums []Checksum
}

// CacheFile is content served from the local cache.
type CacheFile struct {
	File string
	R    ResolvedRepo
}

// Redirect is a permanent redirect to another path under the same repo.
type Redirect struct {
	Path cpath.ContentPath
	R    ResolvedRepo
}

// GeneratedFile is generated content backed by a file.
type GeneratedFile struct {
	File        string
	ContentType ContentType
	R           ResolvedRepo
}

// GeneratedContent is generated content held in memory (index.html, metadata...).
type GeneratedContent struct {
	R           ResolvedRepo
	ContentType ContentType
	Content     string
}

func (c TempFile) ContentRepo() ResolvedRepo         { return c.R }
func (c CacheFile) ContentRepo() ResolvedRepo        { return c.R }
func (c Redirect) ContentRepo() ResolvedRepo         { return c.R }
func (c GeneratedFile) ContentRepo() ResolvedRepo    { return c.R }
func (c GeneratedContent) ContentRepo() ResolvedRepo { return c.R }

// GenerateHTML / GenerateXML mirror RepoContent.generateHtml/generateXml.
func GenerateHTML(repo ResolvedRepo, content string) GeneratedContent {
	return GeneratedContent{R: repo, ContentType: CTHtml, Content: content}
}
func GenerateXML(repo ResolvedRepo, content string) GeneratedContent {
	return GeneratedContent{R: repo, ContentType: CTXml, Content: content}
}

// ---- DownloadResult (sealed) ----

// DownloadResult mirrors ResolvedModel.DownloadResult.
type DownloadResult interface{ isDownloadResult() }

// DownloadSuccess is a downloaded file pending checksum validation.
type DownloadSuccess struct {
	R    ResolvedRepo
	File string
}

// DownloadAsRepoContent is content that bypasses checksum validation.
type DownloadAsRepoContent struct {
	Content RepoContent
}

func (DownloadSuccess) isDownloadResult()       {}
func (DownloadAsRepoContent) isDownloadResult() {}

// ---- interfaces ----

// ContentGenerator mirrors ResolvedModel.ContentGenerator.
type ContentGenerator interface {
	CanGenerateFor(p cpath.ContentPath) bool
	// Generate returns nil RepoContent when nothing is generated.
	Generate(ctx context.Context, context0 string, p cpath.ContentPath, repo ResolvedRepo) (RepoContent, error)
	ExtraEntries(entries []DirectoryEntry, repo ResolvedRepo) []DirectoryEntry
}

// ResolvedRepo mirrors the parts of a8.locus.ResolvedRepo used across packages.
type ResolvedRepo interface {
	Name() string
	// GeneratedChecksumHandlers returns the repo's configured checksum kinds.
	GeneratedChecksumHandlers() []checksum.Kind
	// ContentGenerators returns the repo's content generators (index/metadata/checksum).
	ContentGenerators() []ContentGenerator
	// Entries lists directory entries; found=false distinguishes a missing
	// directory from an empty one (mirrors Option[Vector[DirectoryEntry]]).
	Entries(ctx context.Context, p cpath.ContentPath) (entries []DirectoryEntry, found bool, err error)
	// ResolveContent resolves a path; returns nil when not found.
	ResolveContent(ctx context.Context, p cpath.ContentPath, includeGenerated bool) (RepoContent, error)
	// Put stores a file at the path.
	Put(ctx context.Context, p cpath.ContentPath, srcFile string) (PutResult, error)
	// ClearCache removes cached files at/under the path.
	ClearCache(ctx context.Context, p cpath.ContentPath) ([]ClearedEntry, error)
}
