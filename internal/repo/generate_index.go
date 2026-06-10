package repo

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/accur8/locus-go/internal/cpath"
	"github.com/accur8/locus-go/internal/model"
)

// indexHTMLGenerator mirrors a8.locus.GenerateIndexDotHtml.
type indexHTMLGenerator struct{}

func (indexHTMLGenerator) CanGenerateFor(p cpath.ContentPath) bool {
	return strings.EqualFold(p.Last(), "index.html") || p.IsDir
}

func (indexHTMLGenerator) ExtraEntries(_ []model.DirectoryEntry, _ model.ResolvedRepo) []model.DirectoryEntry {
	return nil
}

const indexTemplate = `<html>
  <head>
    <style>
      table, th, td {
        border: 1px solid black;
        border-collapse: collapse;
      }
      th, td {
        padding-left: 10px;
        padding-right: 10px;
      }
    </style>
  </head>
  <body>
    <br/>
      &nbsp;&nbsp;
      &nbsp;<a href="/repos/">repos</a>
      &nbsp;/&nbsp;
      <a href="/repos/%[1]s/index.html">%[1]s</a>
      &nbsp;&nbsp;--&nbsp;&nbsp;
      %[2]s
    <br/>
    <br/>
    <table>
      <tr>
%[3]s
      </tr>
%[4]s
    </table>
    <br/>
    <br/>
    <a href="/repos/%[1]s/?action=clearcache">clear cache for entire directory</a>
  </body>
</html>`

var indexHeaders = strings.Join([]string{
	"<th>Name</th>",
	"<th>Last Modified</th>",
	"<th>Size</th>",
	"<th>Repo</th>",
	"<th>Clear Cache</th>",
	"<th>Debug</th>",
}, "\n")

func (indexHTMLGenerator) Generate(ctx context.Context, context0 string, contentPath cpath.ContentPath, repo model.ResolvedRepo) (model.RepoContent, error) {
	dir := contentPath
	if !contentPath.IsDir {
		dir = contentPath.Parent()
	}

	breadCrumbs := buildBreadcrumbs(context0, dir)

	rawEntries, _, err := repo.Entries(ctx, dir)
	if err != nil {
		return nil, err
	}
	var extra []model.DirectoryEntry
	for _, g := range repo.ContentGenerators() {
		extra = append(extra, g.ExtraEntries(rawEntries, repo)...)
	}
	all := append(append([]model.DirectoryEntry{}, rawEntries...), extra...)
	sort.SliceStable(all, func(i, j int) bool {
		di, dj := !all[i].IsDirectory, !all[j].IsDirectory
		if di != dj {
			return !di // directories (false) first
		}
		return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name)
	})

	var rows strings.Builder
	for i, e := range all {
		if i > 0 {
			rows.WriteByte('\n')
		}
		rows.WriteString("<tr>")
		rows.WriteString(rowCells(e))
		rows.WriteString("</tr>")
	}

	html := fmt.Sprintf(indexTemplate, repo.Name(), breadCrumbs, indexHeaders, rows.String())
	return model.GenerateHTML(repo, html), nil
}

func buildBreadcrumbs(context0 string, dir cpath.ContentPath) string {
	var links []string
	for i := 1; i <= len(dir.Parts); i++ {
		prefix := dir.Parts[:i]
		links = append(links, "<a href=\""+context0+"/"+strings.Join(prefix, "/")+"/index.html\">"+prefix[len(prefix)-1]+"</a>")
	}
	return strings.Join(links, "&nbsp;/&nbsp;")
}

func rowCells(e model.DirectoryEntry) string {
	var b strings.Builder
	// Name
	b.WriteString("<td>")
	b.WriteString(nameCell(e))
	b.WriteString("</td>")
	// Last Modified
	b.WriteString("<td>")
	if e.LastModified != nil {
		b.WriteString(e.LastModified.String())
	}
	b.WriteString("</td>")
	// Size (right-aligned)
	b.WriteString(`<td align="right">`)
	if e.Size != nil {
		b.WriteString(groupDigits(*e.Size))
		b.WriteString(" bytes")
	}
	b.WriteString("</td>")
	// Repo
	b.WriteString("<td>")
	b.WriteString(repoCell(e))
	b.WriteString("</td>")
	// Clear Cache
	b.WriteString("<td>")
	b.WriteString(`<a href="` + e.Name + `?action=clearcache">clearcache</a>`)
	b.WriteString("</td>")
	// Debug (only for files)
	b.WriteString("<td>")
	if !e.IsDirectory {
		b.WriteString(`<a href="` + e.Name + `?action=debug">debug</a>`)
	}
	b.WriteString("</td>")
	return b.String()
}

func nameCell(e model.DirectoryEntry) string {
	if e.IsDirectory {
		return "<a href='" + e.Name + "/index.html'>" + e.Name + "/</a>"
	}
	return "<a href='" + e.Name + "'>" + e.Name + "</a>"
}

func repoCell(e model.DirectoryEntry) string {
	if e.Generated {
		return "generated"
	}
	if e.DirectURL != nil {
		return "<a href=\"" + e.DirectURL.String() + "\">" + e.Repo.Name() + "</a>"
	}
	return e.Repo.Name()
}

// groupDigits formats with comma thousands separators (NumberFormat en_US).
func groupDigits(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	res := strings.Join(parts, ",")
	if neg {
		res = "-" + res
	}
	return res
}
