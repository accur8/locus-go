package repo

import (
	"context"
	"strings"

	"github.com/accur8/locus-go/internal/checksum"
	"github.com/accur8/locus-go/internal/cpath"
	"github.com/accur8/locus-go/internal/model"
)

// checksumGenerator mirrors a8.locus.ChecksumGenerator.
type checksumGenerator struct{}

var checksumGenExtensions = []string{".jar", ".pom", ".xml"}

func (checksumGenerator) CanGenerateFor(p cpath.ContentPath) bool {
	last := strings.ToLower(p.Last())
	for _, k := range checksum.All {
		if strings.HasSuffix(last, k.ExtensionLc()) {
			return true
		}
	}
	return false
}

func (checksumGenerator) Generate(ctx context.Context, _ string, checksumPath cpath.ContentPath, repo model.ResolvedRepo) (model.RepoContent, error) {
	handler, ok := checksumForPath(checksumPath, repo)
	if !ok {
		return nil, nil
	}
	basePath, ok := checksumPath.DropExtension()
	if !ok {
		return nil, nil
	}
	rc, err := repo.ResolveContent(ctx, basePath, true)
	if err != nil {
		return nil, err
	}
	if rc == nil {
		return nil, nil
	}

	var digest []byte
	switch c := rc.(type) {
	case model.Redirect:
		return nil, nil
	case model.GeneratedFile:
		digest, err = handler.DigestFile(c.File)
	case model.GeneratedContent:
		digest = handler.DigestString(c.Content)
	case model.CacheFile:
		digest, err = handler.DigestFile(c.File)
	case model.TempFile:
		digest, err = handler.DigestFile(c.File)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model.GeneratedContent{R: repo, ContentType: "", Content: checksum.HexString(digest)}, nil
}

// checksumForPath finds the first configured handler whose extension the
// filename ends with (mirrors ChecksumGenerator.checksumForPath).
func checksumForPath(p cpath.ContentPath, repo model.ResolvedRepo) (checksum.Kind, bool) {
	lc := strings.ToLower(p.Last())
	for _, k := range repo.GeneratedChecksumHandlers() {
		if strings.HasSuffix(lc, k.ExtensionLc()) {
			return k, true
		}
	}
	return checksum.Kind{}, false
}

func canGenerateForEntry(name string) bool {
	lc := strings.ToLower(name)
	for _, e := range checksumGenExtensions {
		if strings.HasSuffix(lc, e) {
			return true
		}
	}
	return false
}

func (checksumGenerator) ExtraEntries(entries []model.DirectoryEntry, repo model.ResolvedRepo) []model.DirectoryEntry {
	existing := map[string]bool{}
	for _, e := range entries {
		existing[e.Name] = true
	}
	var out []model.DirectoryEntry
	for _, e := range entries {
		if !canGenerateForEntry(e.Name) {
			continue
		}
		handlers := distinctKinds(append(append([]checksum.Kind{}, e.Repo.GeneratedChecksumHandlers()...), repo.GeneratedChecksumHandlers()...))
		for _, ch := range handlers {
			name := e.Name + "." + ch.Ext
			if existing[name] {
				continue
			}
			size := int64(64)
			out = append(out, model.DirectoryEntry{
				Name:         name,
				IsDirectory:  e.IsDirectory,
				Repo:         e.Repo,
				LastModified: e.LastModified,
				Size:         &size,
				Generated:    true,
				DirectURL:    nil,
			})
		}
	}
	return out
}

func distinctKinds(in []checksum.Kind) []checksum.Kind {
	seen := map[string]bool{}
	var out []checksum.Kind
	for _, k := range in {
		if !seen[k.ExtensionLc()] {
			seen[k.ExtensionLc()] = true
			out = append(out, k)
		}
	}
	return out
}
