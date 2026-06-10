// Package cpath models a repository content path, mirroring the ContentPath /
// UrlPath types in the original Scala (a8.locus.ziohttp.model.ContentPath and
// a8.locus.Dsl.UrlPath). Parts never contain "/" or "\" and never "." / "..".
package cpath

import "strings"

// ContentPath is a path within a repository (below /repos/<name>/).
type ContentPath struct {
	Parts []string
	IsDir bool
}

// New builds a ContentPath, scrubbing unsafe / relative segments exactly like
// the Scala ContentPath.apply.
func New(parts []string, isDir bool) ContentPath {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "." || s == ".." || strings.Contains(s, "/") || strings.Contains(s, "\\") {
			continue
		}
		out = append(out, p)
	}
	return ContentPath{Parts: out, IsDir: isDir}
}

// Parse parses a "/"-separated path, folding "." and "..", and treats a
// trailing slash as a directory (mirrors UrlPath.parse).
func Parse(path string) ContentPath {
	p := strings.TrimSpace(path)
	isDir := strings.HasSuffix(p, "/")
	var acc []string
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".":
			// drop
		case "..":
			if len(acc) > 0 {
				acc = acc[:len(acc)-1]
			}
		default:
			acc = append(acc, seg)
		}
	}
	return ContentPath{Parts: acc, IsDir: isDir}
}

// Empty is the empty directory path.
var Empty = ContentPath{Parts: nil, IsDir: false}

// Last returns the final segment (empty if none).
func (c ContentPath) Last() string {
	if len(c.Parts) == 0 {
		return ""
	}
	return c.Parts[len(c.Parts)-1]
}

// IsEmpty reports whether there are no parts.
func (c ContentPath) IsEmpty() bool { return len(c.Parts) == 0 }

// Parent returns the containing directory path.
func (c ContentPath) Parent() ContentPath {
	if len(c.Parts) == 0 {
		return ContentPath{Parts: nil, IsDir: true}
	}
	np := append([]string(nil), c.Parts[:len(c.Parts)-1]...)
	return ContentPath{Parts: np, IsDir: true}
}

// Extension returns the substring after the last '.' in the last segment, or ""
// (mirrors Scala: lastIndexOf('.') >= 0 -> substring(i+1)).
func (c ContentPath) Extension() string {
	last := c.Last()
	i := strings.LastIndex(last, ".")
	if i >= 0 {
		return last[i+1:]
	}
	return ""
}

// DropExtension removes the extension; ok=false when there is none, mirroring
// Scala which only drops when the '.' index is > 0.
func (c ContentPath) DropExtension() (ContentPath, bool) {
	last := c.Last()
	i := strings.LastIndex(last, ".")
	if i > 0 {
		np := append([]string(nil), c.Parts...)
		np[len(np)-1] = last[:i]
		return ContentPath{Parts: np, IsDir: c.IsDir}, true
	}
	return ContentPath{}, false
}

// AppendExtension appends ".<ext>" to the last segment.
func (c ContentPath) AppendExtension(ext string) ContentPath {
	np := append([]string(nil), c.Parts...)
	if len(np) > 0 {
		np[len(np)-1] = np[len(np)-1] + "." + ext
	}
	return ContentPath{Parts: np, IsDir: c.IsDir}
}

// Append concatenates another path's parts; the suffix's IsDir wins.
func (c ContentPath) Append(suffix ContentPath) ContentPath {
	np := make([]string, 0, len(c.Parts)+len(suffix.Parts))
	np = append(np, c.Parts...)
	np = append(np, suffix.Parts...)
	return ContentPath{Parts: np, IsDir: suffix.IsDir}
}

// AsDirectory / AsFile flip the directory flag.
func (c ContentPath) AsDirectory() ContentPath { c.IsDir = true; return c }
func (c ContentPath) AsFile() ContentPath      { c.IsDir = false; return c }

// FullPath joins parts with "/" and appends a trailing "/" for directories.
func (c ContentPath) FullPath() string {
	s := strings.Join(c.Parts, "/")
	if c.IsDir {
		return s + "/"
	}
	return s
}

func (c ContentPath) String() string { return c.FullPath() }
