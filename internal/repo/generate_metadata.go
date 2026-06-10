package repo

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/accur8/locus-go/internal/cpath"
	"github.com/accur8/locus-go/internal/javatime"
	"github.com/accur8/locus-go/internal/mavenversion"
	"github.com/accur8/locus-go/internal/model"
)

// mavenMetadataGenerator mirrors a8.locus.GenerateMavenMetadata.
type mavenMetadataGenerator struct{}

func (mavenMetadataGenerator) CanGenerateFor(p cpath.ContentPath) bool {
	return strings.EqualFold(p.Last(), "maven-metadata.xml")
}

func (mavenMetadataGenerator) ExtraEntries(_ []model.DirectoryEntry, _ model.ResolvedRepo) []model.DirectoryEntry {
	return nil
}

type versionEntry struct {
	pv   mavenversion.ParsedVersion
	dt   javatime.DateTime
	name string
}

func (mavenMetadataGenerator) Generate(ctx context.Context, _ string, contentPath cpath.ContentPath, repo model.ResolvedRepo) (model.RepoContent, error) {
	entries, found, err := repo.Entries(ctx, contentPath.Parent())
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	var ves []versionEntry
	for _, e := range entries {
		if !e.IsDirectory {
			continue
		}
		pv, ok := mavenversion.Parse(e.Name)
		if !ok {
			continue
		}
		dt := javatime.Empty()
		if pv.Build != nil {
			ts := pv.Build.Ts
			sec := 0
			if ts.Second != nil {
				sec = *ts.Second
			}
			dt = javatime.FromYMDHMS(ts.Year, time.Month(ts.Month), ts.Day, ts.Hour, ts.Minute, sec)
		}
		ves = append(ves, versionEntry{pv: pv, dt: dt, name: e.Name})
	}
	// Scala would throw on sortedEntries.last for an empty list; we return
	// "not found" instead of crashing on a directory with no parseable versions.
	if len(ves) == 0 {
		return nil, nil
	}

	sort.SliceStable(ves, func(i, j int) bool {
		if c := mavenversion.Compare(ves[i].pv, ves[j].pv); c != 0 {
			return c < 0
		}
		if ei, ej := ves[i].dt.Epoch(), ves[j].dt.Epoch(); ei != ej {
			return ei < ej
		}
		return ves[i].name < ves[j].name
	})

	last := ves[len(ves)-1]
	latest := last.name
	lastUpdated := last.dt.MavenLastUpdated()
	artifactID := contentPath.Last()
	groupID := contentPath.Parent().FullPath()

	var versionsBlock strings.Builder
	for i, v := range ves {
		if i > 0 {
			versionsBlock.WriteByte('\n')
		}
		versionsBlock.WriteString("        <version>")
		versionsBlock.WriteString(v.name)
		versionsBlock.WriteString("</version>")
	}

	xml := `<?xml version="1.0" encoding="UTF-8"?>

  <metadata modelVersion="1.1.0">
    <groupId>` + groupID + `</groupId>
    <artifactId>` + artifactID + `</artifactId>
    <version>` + latest + `</version>
    <versioning>
      <latest>` + latest + `</latest>
      <release>` + latest + `</release>
      <versions>
` + versionsBlock.String() + `
      </versions>
    </versioning>
    <lastUpdated>` + lastUpdated + `</lastUpdated>
  </metadata>`

	return model.GenerateXML(repo, xml), nil
}
