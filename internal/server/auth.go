package server

import (
	"encoding/base64"
	"net"
	"net/http"
	"strings"

	"github.com/accur8/locus-go/internal/config"
)

// resolveUser mirrors UserService.resolveUser: try the Authorization header,
// then fall back to anonymous-by-subnet (even if the header was present but
// invalid, matching the Scala orElse).
func (s *Server) resolveUser(r *http.Request) (config.User, bool) {
	if authz := r.Header.Get("Authorization"); authz != "" {
		if u, ok := s.authenticate(authz); ok {
			return u, true
		}
	}
	return s.anonymousLogin(r)
}

// authenticate mirrors UserService.authenticate. Name and password are compared
// case-insensitively (the Scala uses =:= / equalsIgnoreCase).
func (s *Server) authenticate(header string) (config.User, bool) {
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	if len(parts) != 2 || parts[0] != "Basic" {
		return config.User{}, false
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return config.User{}, false
	}
	creds := strings.SplitN(string(decoded), ":", 2)
	if len(creds) != 2 {
		return config.User{}, false
	}
	for _, u := range s.cfg.Users {
		if strings.EqualFold(u.Name, creds[0]) && strings.EqualFold(u.Password, creds[1]) {
			return u, true
		}
	}
	return config.User{}, false
}

// anonymousLogin mirrors UserService.anonymousLogin.
func (s *Server) anonymousLogin(r *http.Request) (config.User, bool) {
	ip := remoteIP(r)
	if ip == nil {
		return config.User{}, false
	}
	if s.subnets.IsInSubnet(ip, r.Header.Get("X-Forwarded-For")) {
		return config.AnonymousUser, true
	}
	return config.User{}, false
}

// requirePrivilege returns a 401 response if the request lacks the privilege,
// or nil if access is granted (mirrors UserService.requirePrivilege).
func (s *Server) requirePrivilege(r *http.Request, priv config.UserPrivilege) *response {
	user, ok := s.resolveUser(r)
	if ok && user.HasPrivilege(priv) {
		return nil
	}
	resp := newResp(http.StatusUnauthorized)
	// zio-http's WWWAuthenticate.Basic renders the charset parameter (default
	// "UTF-8"); match it exactly for wire parity.
	resp.header.Set("WWW-Authenticate", `Basic realm="`+s.cfg.Realm+`", UTF-8="UTF-8"`)
	return resp
}

func remoteIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}
