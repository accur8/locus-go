// Package upstream contains the clients that talk to backing stores: an HTTP
// client (mirrors a8.locus.UrlAssist), an S3 client (mirrors a8.locus.S3Assist),
// and the maven index.html scraper (mirrors a8.locus.ReadMavenIndexDotHtml).
package upstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/accur8/locus-go/internal/uri"
)

// BasicAuth carries HTTP basic-auth credentials.
type BasicAuth struct {
	Username string
	Password string
}

// TempFileFunc creates a fresh temp file path (parent dirs created).
type TempFileFunc func() (string, error)

// Response mirrors UrlAssist.Response. BodyFile is "" when there is no body
// (status >= 400, or a redirect).
type Response struct {
	Status        int
	StatusMessage string
	Headers       http.Header
	BodyFile      string
}

// Header returns the first value of a response header (case-insensitive).
func (r *Response) Header(name string) string { return r.Headers.Get(name) }

// HTTPClient mirrors a8.locus.UrlAssist.
type HTTPClient struct {
	hc *http.Client
}

// NewHTTPClient builds an HTTP client that does not auto-follow redirects.
func NewHTTPClient() *HTTPClient {
	return &HTTPClient{
		hc: &http.Client{
			Timeout: 5 * time.Minute,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // never auto-follow; we handle 302s
			},
		},
	}
}

const maxRedirects = 10

// Get performs a GET. When followRedirects is true, 302s are followed (up to
// maxRedirects); otherwise a 302 is returned to the caller (mirrors UrlAssist).
func (c *HTTPClient) Get(ctx context.Context, u uri.Uri, auth *BasicAuth, followRedirects bool, newTemp TempFileFunc) (*Response, error) {
	redirectsLeft := 0
	if followRedirects {
		redirectsLeft = maxRedirects
	}
	return c.execute(ctx, u, auth, redirectsLeft, newTemp)
}

func (c *HTTPClient) execute(ctx context.Context, u uri.Uri, auth *BasicAuth, redirectsLeft int, newTemp TempFileFunc) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if auth != nil {
		req.SetBasicAuth(auth.Username, auth.Password)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusFound && redirectsLeft > 0 { // 302
		location := resp.Header.Get("Location")
		if location == "" {
			return nil, fmt.Errorf("invalid redirect from %s", u.String())
		}
		var next uri.Uri
		switch {
		case strings.HasPrefix(location, "/"):
			n, ok := uri.Parse(u.Root().String() + location)
			if !ok {
				return nil, fmt.Errorf("invalid redirect location %q", location)
			}
			next = n
		case strings.HasPrefix(location, "http"):
			n, ok := uri.Parse(location)
			if !ok {
				return nil, fmt.Errorf("invalid redirect location %q", location)
			}
			next = n
		default:
			return nil, fmt.Errorf("invalid location %q", location)
		}
		io.Copy(io.Discard, resp.Body)
		return c.execute(ctx, next, auth, redirectsLeft-1, newTemp)
	}

	statusMsg := strings.TrimSpace(strings.TrimPrefix(resp.Status, fmt.Sprintf("%d", resp.StatusCode)))
	out := &Response{Status: resp.StatusCode, StatusMessage: statusMsg, Headers: resp.Header}

	if resp.StatusCode >= 400 {
		io.Copy(io.Discard, resp.Body)
		return out, nil
	}

	dest, err := newTemp()
	if err != nil {
		return nil, err
	}
	f, err := os.Create(dest)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	out.BodyFile = dest
	return out, nil
}

// ReadFileString reads a file as a UTF-8 string.
func ReadFileString(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
