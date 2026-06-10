// Package uri is a small URI model mirroring a8.locus.model.Uri (which wraps
// sttp.model.Uri in the original Scala). It supports http(s) and s3 schemes and
// the path-join (`/`) semantics the repo code relies on.
package uri

import (
	"net/url"
	"strings"
)

// Uri is an immutable parsed URI.
type Uri struct {
	Scheme   string
	Host     string // host[:port]
	User     string
	Password string // empty if absent
	HasUser  bool
	HasPass  bool
	Segments []string // path segments, no empty elements
	// TrailingSlash preserves directory semantics (a path ending in "/"). This
	// matters for upstream directory listings: repo1.maven.org 302-redirects a
	// no-slash directory URL to the slash form, so we must keep the slash to
	// avoid a redirect loop (sttp preserves it in the original Scala).
	TrailingSlash bool
}

// Parse parses a URI string. It panics-free: returns ok=false on failure.
func Parse(s string) (Uri, bool) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Scheme == "" {
		return Uri{}, false
	}
	out := Uri{
		Scheme:        u.Scheme,
		Host:          u.Host,
		Segments:      splitSegments(u.Path),
		TrailingSlash: len(u.Path) > 1 && strings.HasSuffix(u.Path, "/"),
	}
	if u.User != nil {
		out.HasUser = true
		out.User = u.User.Username()
		if p, ok := u.User.Password(); ok {
			out.HasPass = true
			out.Password = p
		}
	}
	return out, true
}

// MustParse parses or panics; use only for config values already validated.
func MustParse(s string) Uri {
	u, ok := Parse(s)
	if !ok {
		panic("invalid uri: " + s)
	}
	return u
}

func splitSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Root returns the scheme://host with no path (mirrors Scala Uri.root).
func (u Uri) Root() Uri {
	r := u
	r.Segments = nil
	r.TrailingSlash = false
	return r
}

// Path returns the path joined by "/", and whether a path is present
// (mirrors Scala Uri.path: Option[String]).
func (u Uri) Path() (string, bool) {
	if len(u.Segments) == 0 {
		return "", false
	}
	return strings.Join(u.Segments, "/"), true
}

// Join appends path segments parsed from suffix (split on "/"); the result is a
// directory URI iff suffix ends with "/".
func (u Uri) Join(suffix string) Uri {
	r := u.JoinParts(splitSegments(suffix))
	r.TrailingSlash = strings.HasSuffix(strings.TrimSpace(suffix), "/")
	return r
}

// JoinParts appends the given already-split path parts (not a directory).
func (u Uri) JoinParts(parts []string) Uri {
	r := u
	ns := make([]string, 0, len(u.Segments)+len(parts))
	ns = append(ns, u.Segments...)
	for _, p := range parts {
		if p != "" {
			ns = append(ns, p)
		}
	}
	r.Segments = ns
	r.TrailingSlash = false
	return r
}

func (u Uri) userInfo() string {
	if !u.HasUser {
		return ""
	}
	if u.HasPass {
		return u.User + ":" + u.Password + "@"
	}
	return u.User + "@"
}

// String renders the URI (mirrors sttp's rendering for the cases we use).
func (u Uri) String() string {
	var b strings.Builder
	b.WriteString(u.Scheme)
	b.WriteString("://")
	b.WriteString(u.userInfo())
	b.WriteString(u.Host)
	for _, s := range u.Segments {
		b.WriteByte('/')
		b.WriteString(s)
	}
	if u.TrailingSlash {
		b.WriteByte('/')
	}
	return b.String()
}
