package upstream

import (
	"strconv"
	"strings"

	"github.com/accur8/locus-go/internal/javatime"
	"github.com/accur8/locus-go/internal/model"
	"github.com/accur8/locus-go/internal/uri"
	"golang.org/x/net/html"
)

// ParseMavenIndex mirrors a8.locus.ReadMavenIndexDotHtml.parse. It scrapes an
// upstream Maven directory index.html into DirectoryEntry values. It locates the
// first <a>, then walks its parent's children, pairing each <a> with the text
// node that follows it (which holds "date time size").
func ParseMavenIndex(baseURI uri.Uri, htmlStr string, repo model.ResolvedRepo) ([]model.DirectoryEntry, error) {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return nil, err
	}
	firstA := findFirstElement(doc, "a")
	if firstA == nil {
		return nil, nil
	}
	parent := firstA.Parent
	var entries []model.DirectoryEntry
	for ch := parent.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type != html.ElementNode || ch.Data != "a" {
			continue
		}
		href := getAttr(ch, "href")
		isDir := strings.HasSuffix(href, "/")
		name := href
		if isDir {
			name = href[:len(href)-1]
		}
		text := ""
		if ns := ch.NextSibling; ns != nil && ns.Type == html.TextNode {
			text = ns.Data
		}
		tokens := strings.Fields(strings.TrimSpace(text))

		entry, ok := buildEntry(baseURI, repo, name, isDir, tokens)
		if ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func buildEntry(baseURI uri.Uri, repo model.ResolvedRepo, name string, isDir bool, tokens []string) (model.DirectoryEntry, bool) {
	directURL := baseURI.Join(name)
	switch {
	case name == ".." && isDir && len(tokens) == 0:
		return model.DirectoryEntry{}, false

	case len(tokens) == 3 && tokens[2] == "-":
		dt, err := javatime.UberParse(tokens[0] + " " + tokens[1])
		if err != nil {
			return model.DirectoryEntry{}, false
		}
		return model.DirectoryEntry{Name: name, IsDirectory: isDir, Repo: repo, LastModified: &dt, DirectURL: &directURL}, true

	case len(tokens) == 2 && tokens[0] == "-" && tokens[1] == "-":
		return model.DirectoryEntry{Name: name, IsDirectory: isDir, Repo: repo, DirectURL: &directURL}, true

	case len(tokens) == 3:
		dt, err := javatime.UberParse(tokens[0] + " " + tokens[1])
		if err != nil {
			return model.DirectoryEntry{}, false
		}
		size, err := strconv.ParseInt(tokens[2], 10, 64)
		if err != nil {
			return model.DirectoryEntry{}, false
		}
		return model.DirectoryEntry{Name: name, IsDirectory: isDir, Repo: repo, LastModified: &dt, Size: &size, DirectURL: &directURL}, true

	default:
		return model.DirectoryEntry{}, false
	}
}

func findFirstElement(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findFirstElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
