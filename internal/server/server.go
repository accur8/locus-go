// Package server implements the HTTP layer: routing, the repo handler
// (GET/HEAD/PUT + ?action=debug/clearcache), the root and list-repos pages, and
// basic-auth/anonymous authorization. Mirrors a8.locus.ziohttp.*.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/accur8/locus-go/internal/config"
	"github.com/accur8/locus-go/internal/cpath"
	"github.com/accur8/locus-go/internal/model"
	"github.com/accur8/locus-go/internal/repo"
)

// Server holds the HTTP layer dependencies.
type Server struct {
	cfg     *config.LocusConfig
	model   *repo.Model
	subnets config.SubnetManager
}

// New builds a Server.
func New(cfg *config.LocusConfig, m *repo.Model, subnets config.SubnetManager) *Server {
	return &Server{cfg: cfg, model: m, subnets: subnets}
}

// Handler returns the root http.Handler.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serve)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	repoLog := model.NewRepoLog(func(level, msg string, err error) {
		// tee important lines to the process logger at debug level
		slog.Debug("repo", "level", level, "msg", msg, "err", err)
	})
	ctx := model.WithRepoLog(r.Context(), repoLog)

	context := fmt.Sprintf("%s %s", r.Method, r.URL.RequestURI())
	slog.Debug("request", "ctx", context)

	resp, err := s.route(ctx, r)
	if err != nil {
		slog.Warn("error servicing request", "ctx", context, "err", err)
		// Scala's HttpResponses.text sets a bare "text/plain" (no charset) on the
		// unexpected-error 500, unlike the charset-defaulted string responses.
		resp = stringResp(http.StatusInternalServerError, err.Error(), "text/plain")
	}
	resp.write(w, r.Method == http.MethodHead)
	slog.Debug("completed", "status", resp.status, "ctx", context)
}

func (s *Server) route(ctx context.Context, r *http.Request) (*response, error) {
	cp := cpath.Parse(r.URL.Path)
	parts := cp.Parts
	method := r.Method

	// repo handler: /repos/<name>/... for GET/PUT/HEAD when the repo exists
	if len(parts) >= 2 && strings.EqualFold(parts[0], "repos") &&
		(method == http.MethodGet || method == http.MethodPut || method == http.MethodHead) {
		if rp := s.model.RepoByName(parts[1]); rp != nil {
			contentPath := cpath.New(parts[2:], cp.IsDir)
			return s.handleRepo(ctx, r, rp, contentPath)
		}
	}

	// list repos: /repos or /repos/index.html
	if method == http.MethodGet && len(parts) >= 1 && strings.EqualFold(parts[0], "repos") {
		if len(parts) == 1 || (len(parts) == 2 && strings.EqualFold(parts[1], "index.html")) {
			return s.handleListRepos(), nil
		}
	}

	// root: / or /index.html
	if method == http.MethodGet && (len(parts) == 0 || (len(parts) == 1 && strings.EqualFold(parts[0], "index.html"))) {
		return s.handleRoot(), nil
	}

	return textResp(http.StatusNotFound,
		fmt.Sprintf("no match found for request %s with http method %s", r.URL.RequestURI(), method)), nil
}

// ---- repo handler ----

func (s *Server) handleRepo(ctx context.Context, r *http.Request, rp *repo.Repo, cp cpath.ContentPath) (*response, error) {
	switch r.Method {
	case http.MethodGet:
		if resp := s.requirePrivilege(r, config.PrivRead); resp != nil {
			return resp, nil
		}
		return s.doGet(ctx, r, rp, cp)
	case http.MethodPut:
		if resp := s.requirePrivilege(r, config.PrivWrite); resp != nil {
			return resp, nil
		}
		return s.doPut(ctx, r, rp, cp)
	case http.MethodHead:
		if resp := s.requirePrivilege(r, config.PrivRead); resp != nil {
			return resp, nil
		}
		return s.doHead(ctx, rp, cp)
	default:
		return textResp(http.StatusMethodNotAllowed, r.Method+" is not allowed"), nil
	}
}

func (s *Server) doGet(ctx context.Context, r *http.Request, rp *repo.Repo, cp cpath.ContentPath) (*response, error) {
	q := r.URL.Query()
	// Scala distinguishes an absent action param (-> normal GET) from a present
	// one (incl. empty value -> unknown-action 404).
	if _, present := q["action"]; !present {
		return s.doGet0(ctx, r, rp, cp)
	}
	action := strings.ToLower(q.Get("action"))
	switch action {
	case "debug":
		return s.doDebug(ctx, rp, cp)
	case "clearcache":
		return s.doClearCache(ctx, rp, cp)
	default:
		return textResp(http.StatusNotFound,
			fmt.Sprintf("no action named %s found valid actions are debug and clearcache", action)), nil
	}
}

func (s *Server) doGet0(ctx context.Context, r *http.Request, rp *repo.Repo, cp cpath.ContentPath) (*response, error) {
	rc, err := rp.ResolveContent(ctx, cp, true)
	if err != nil {
		return nil, err
	}
	switch c := rc.(type) {
	case nil:
		return textResp(http.StatusNotFound, "unable to resolve "+r.URL.Path), nil
	case model.CacheFile:
		return responseFromFile(c.File, defaultContentType(cp.Last()))
	case model.TempFile:
		return responseFromFile(c.File, defaultContentType(cp.Last()))
	case model.GeneratedContent:
		return stringResp(http.StatusOK, c.Content, c.ContentType), nil
	case model.GeneratedFile:
		return responseFromFile(c.File, c.ContentType)
	case model.Redirect:
		return redirectResp(buildRedirectLocation(r.URL.Path, c.Path)), nil
	default:
		return textResp(http.StatusNotFound, "unable to resolve "+r.URL.Path), nil
	}
}

// buildRedirectLocation mirrors permanentRedirect(rootPath.append(path)): the
// request path joined with the redirect's relative path, leading "/".
func buildRedirectLocation(requestPath string, redirect cpath.ContentPath) string {
	base := cpath.Parse(requestPath)
	combined := base.Append(redirect)
	return "/" + combined.FullPath()
}

func (s *Server) doHead(ctx context.Context, rp *repo.Repo, cp cpath.ContentPath) (*response, error) {
	rc, err := rp.ResolveContent(ctx, cp, true)
	if err != nil {
		return nil, err
	}
	if rc != nil {
		return okEmpty(), nil
	}
	return notFoundEmpty(), nil
}

func (s *Server) doPut(ctx context.Context, r *http.Request, rp *repo.Repo, cp cpath.ContentPath) (*response, error) {
	var result *response
	err := s.model.WithWorkDir(func(dir string) error {
		tmp := dir + "/" + cp.Last()
		if err := writeBodyToFile(r, tmp); err != nil {
			return err
		}
		res, err := rp.Put(ctx, cp, tmp)
		if err != nil {
			return err
		}
		switch res {
		case model.PutSuccess:
			result = okEmpty()
		case model.PutAlreadyExists:
			result = stringResp(http.StatusConflict, "path already exists", "")
		case model.PutNotAllowed:
			result = stringResp(http.StatusMethodNotAllowed, "PUT method not supported on this repo", "")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Server) doDebug(ctx context.Context, rp *repo.Repo, cp cpath.ContentPath) (*response, error) {
	log := model.LogFrom(ctx)
	if _, err := rp.ResolveContent(ctx, cp, true); err != nil {
		log.Error("http request failed", err)
	}
	return stringResp(http.StatusOK, strings.Join(log.Lines(), "\n"), ""), nil
}

func (s *Server) doClearCache(ctx context.Context, rp *repo.Repo, cp cpath.ContentPath) (*response, error) {
	cleared, err := rp.ClearCache(ctx, cp)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(cleared))
	for _, c := range cleared {
		lines = append(lines, c.Repo.Name()+" : "+c.Path)
	}
	return stringResp(http.StatusOK, strings.Join(lines, "\n"), ""), nil
}

// ---- root / list repos ----

const rootHTML = `<html>
  <body>
    <a href="/versionsVersion">versions version</a><br/>
    <a href="/repos/">repos</a><br/>
  </body>
</html>`

func (s *Server) handleRoot() *response { return htmlResp(rootHTML) }

func (s *Server) handleListRepos() *response {
	links := make([]string, 0, len(s.model.ResolvedRepos()))
	for _, rp := range s.model.ResolvedRepos() {
		links = append(links, "<a href='/repos/"+rp.Name()+"/index.html'>"+rp.Name()+"</a><br/>")
	}
	return htmlResp("<html><body>" + strings.Join(links, "\n") + "</body></html>")
}
