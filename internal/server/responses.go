package server

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/accur8/locus-go/internal/checksum"
)

// response is a buffered description of an HTTP response. Exactly one of body /
// bodyFile / redirect is used.
type response struct {
	status   int
	header   http.Header
	body     []byte
	bodyFile string
	redirect string
}

func newResp(status int) *response { return &response{status: status, header: http.Header{}} }

// stringResp builds a string body. An empty contentType becomes
// "text/plain; charset=utf-8" (matching zio-http's default for string bodies).
func stringResp(status int, body, contentType string) *response {
	r := newResp(status)
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	r.header.Set("Content-Type", contentType)
	r.body = []byte(body)
	return r
}

func htmlResp(body string) *response { return stringResp(http.StatusOK, body, "text/html") }

func okEmpty() *response       { return newResp(http.StatusOK) }
func notFoundEmpty() *response { return newResp(http.StatusNotFound) }

func textResp(status int, body string) *response { return stringResp(status, body, "") }

// responseFromFile serves a file with x-checksum-md5/sha1 headers and a content
// type (explicit, or derived from the extension), mirroring responseFromFile.
func responseFromFile(file, contentType string) (*response, error) {
	md5b, err := checksum.Md5.DigestFile(file)
	if err != nil {
		return nil, err
	}
	sha1b, err := checksum.Sha1.DigestFile(file)
	if err != nil {
		return nil, err
	}
	r := newResp(http.StatusOK)
	if contentType != "" {
		r.header.Set("Content-Type", contentType)
	}
	r.header.Set("x-checksum-md5", checksum.HexString(md5b))
	r.header.Set("x-checksum-sha1", checksum.HexString(sha1b))
	r.bodyFile = file
	return r, nil
}

// defaultContentType mirrors RepoHttpHandler.defaultContentType. We derive it
// from the requested name rather than the (often extension-less temp) file path,
// so a .jar served on a cache miss is still application/java-archive — matching
// the live server's steady-state behaviour.
func defaultContentType(name string) string {
	lc := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lc, ".jar"):
		return "application/java-archive"
	case strings.HasSuffix(lc, ".pom"):
		return "text/html"
	default:
		return ""
	}
}

func redirectResp(location string) *response {
	r := newResp(http.StatusMovedPermanently)
	r.redirect = location
	return r
}

// write renders the response. For HEAD requests the body is suppressed.
func (r *response) write(w http.ResponseWriter, head bool) {
	h := w.Header()
	for k, vs := range r.header {
		h[k] = vs
	}
	if r.redirect != "" {
		h.Set("Location", r.redirect)
		w.WriteHeader(r.status)
		return
	}
	if r.bodyFile != "" {
		f, err := os.Open(r.bodyFile)
		if err != nil {
			http.Error(w, "unable to open file", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		if fi, e := f.Stat(); e == nil {
			h.Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
		}
		w.WriteHeader(r.status)
		if !head {
			io.Copy(w, f)
		}
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(r.body)))
	w.WriteHeader(r.status)
	if !head {
		w.Write(r.body)
	}
}
