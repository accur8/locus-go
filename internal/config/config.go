// Package config loads and models the Locus configuration, mirroring
// a8.locus.Config. It reads the production HOCON config unchanged.
package config

import (
	"fmt"
	"strings"

	"github.com/accur8/locus-go/internal/hocon"
	"github.com/accur8/locus-go/internal/uri"
)

// DefaultAppPath is the HOCON path holding the LocusConfig object.
const DefaultAppPath = "locus.server.app"

// UserPrivilege mirrors Config.UserPrivilege (ordinal-based).
type UserPrivilege int

const (
	PrivRead  UserPrivilege = 1
	PrivWrite UserPrivilege = 2
	PrivAdmin UserPrivilege = 3
)

// ParsePrivilege parses a privilege name (case-insensitive); defaults to Read.
func ParsePrivilege(s string) UserPrivilege {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "write":
		return PrivWrite
	case "admin":
		return PrivAdmin
	default:
		return PrivRead
	}
}

// User mirrors Config.User.
type User struct {
	Name      string
	Password  string
	Privilege UserPrivilege
}

// HasPrivilege mirrors User.hasPrivilege.
func (u User) HasPrivilege(p UserPrivilege) bool { return int(p) <= int(u.Privilege) }

// AnonymousUser mirrors Config.User.anonymous.
var AnonymousUser = User{Name: "anonymous", Password: "", Privilege: PrivRead}

// S3Config mirrors Config.S3Config.
type S3Config struct {
	AccessKey string
	SecretKey string
}

// RepoConfig mirrors the Config.Repo union (multiplexer | url | local).
type RepoConfig struct {
	Type string // "multiplexer" | "url" | "local"
	Name string

	// url
	URL    uri.Uri
	HasURL bool

	// multiplexer
	Repos            []string
	RepoForWrites    string
	HasRepoForWrites bool

	// local
	Directory string

	GeneratedChecksums []string
}

// LocusConfig mirrors Config.LocusConfig.
type LocusConfig struct {
	Protocol             string
	ProxyServerAddresses []string
	AnonymousSubnets     []string
	DataDirectory        string
	S3                   *S3Config
	Repos                []RepoConfig
	Users                []User
	NoCacheFiles         []string
	Port                 int
	KeepAlive            bool
	VersionsVersion      string
	Realm                string

	noCacheSet map[string]bool
}

// IsNoCacheFile reports whether a filename is configured as non-cacheable
// (case-insensitive), mirroring noCacheFilesSet.
func (c *LocusConfig) IsNoCacheFile(name string) bool {
	return c.noCacheSet[strings.ToLower(name)]
}

// Load reads the config file and extracts the LocusConfig at appPath
// (DefaultAppPath if empty).
func Load(path, appPath string) (*LocusConfig, error) {
	if appPath == "" {
		appPath = DefaultAppPath
	}
	root, err := hocon.ParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	appV := hocon.Lookup(root, appPath)
	app, ok := hocon.AsObject(appV)
	if !ok {
		return nil, fmt.Errorf("config path %q not found or not an object in %s", appPath, path)
	}
	return fromObject(app)
}

func fromObject(app map[string]hocon.Value) (*LocusConfig, error) {
	c := &LocusConfig{
		Protocol:             "https",
		ProxyServerAddresses: []string{"127.0.0.0/8"},
		DataDirectory:        "data",
		Realm:                "Accur8 Repo",
		KeepAlive:            false,
	}

	if v, ok := hocon.AsString(app["protocol"]); ok {
		c.Protocol = v
	}
	if arr, ok := hocon.AsArray(app["proxyServerAddresses"]); ok {
		c.ProxyServerAddresses = toStrings(arr)
	}
	if arr, ok := hocon.AsArray(app["anonymousSubnets"]); ok {
		c.AnonymousSubnets = toStrings(arr)
	}
	if v, ok := hocon.AsString(app["dataDirectory"]); ok {
		c.DataDirectory = v
	}
	if v, ok := hocon.AsString(app["realm"]); ok {
		c.Realm = v
	}
	if v, ok := hocon.AsString(app["protocol"]); ok {
		c.Protocol = v
	}
	if v, ok := hocon.AsString(app["versionsVersion"]); ok {
		c.VersionsVersion = v
	}
	if b, ok := hocon.ToBool(app["keepAlive"]); ok {
		c.KeepAlive = b
	}
	if n, ok := hocon.ToInt(app["port"]); ok {
		c.Port = n
	} else {
		return nil, fmt.Errorf("config: port is required")
	}
	if s3o, ok := hocon.AsObject(app["s3"]); ok {
		ak, _ := hocon.AsString(s3o["accessKey"])
		sk, _ := hocon.AsString(s3o["secretKey"])
		c.S3 = &S3Config{AccessKey: ak, SecretKey: sk}
	}
	if arr, ok := hocon.AsArray(app["noCacheFiles"]); ok {
		c.NoCacheFiles = toStrings(arr)
	}

	// repos
	reposArr, ok := hocon.AsArray(app["repos"])
	if !ok {
		return nil, fmt.Errorf("config: repos is required")
	}
	for i, rv := range reposArr {
		ro, ok := hocon.AsObject(rv)
		if !ok {
			return nil, fmt.Errorf("config: repos[%d] is not an object", i)
		}
		rc, err := repoFromObject(ro)
		if err != nil {
			return nil, fmt.Errorf("config: repos[%d]: %w", i, err)
		}
		c.Repos = append(c.Repos, rc)
	}

	// users
	if usersArr, ok := hocon.AsArray(app["users"]); ok {
		for i, uv := range usersArr {
			uo, ok := hocon.AsObject(uv)
			if !ok {
				return nil, fmt.Errorf("config: users[%d] is not an object", i)
			}
			name, _ := hocon.AsString(uo["name"])
			pass, _ := hocon.AsString(uo["password"])
			priv := PrivRead
			if pv, ok := hocon.AsString(uo["privilege"]); ok {
				priv = ParsePrivilege(pv)
			}
			c.Users = append(c.Users, User{Name: name, Password: pass, Privilege: priv})
		}
	}

	c.noCacheSet = map[string]bool{}
	for _, n := range c.NoCacheFiles {
		c.noCacheSet[strings.ToLower(n)] = true
	}

	return c, nil
}

func repoFromObject(ro map[string]hocon.Value) (RepoConfig, error) {
	typ, _ := hocon.AsString(ro["_type"])
	name, _ := hocon.AsString(ro["name"])
	rc := RepoConfig{Type: strings.ToLower(typ), Name: name}

	if arr, ok := hocon.AsArray(ro["generatedChecksums"]); ok {
		rc.GeneratedChecksums = toStrings(arr)
	} else {
		rc.GeneratedChecksums = []string{"sha256"}
	}

	switch rc.Type {
	case "multiplexer":
		if arr, ok := hocon.AsArray(ro["repos"]); ok {
			rc.Repos = toStrings(arr)
		}
		if v, ok := hocon.AsString(ro["repoForWrites"]); ok {
			rc.RepoForWrites = v
			rc.HasRepoForWrites = true
		}
	case "url":
		us, ok := hocon.AsString(ro["url"])
		if !ok {
			return rc, fmt.Errorf("url repo %q missing url", name)
		}
		u, ok := uri.Parse(us)
		if !ok {
			return rc, fmt.Errorf("url repo %q has invalid url %q", name, us)
		}
		rc.URL = u
		rc.HasURL = true
	case "local":
		dir, _ := hocon.AsString(ro["directory"])
		rc.Directory = dir
	default:
		return rc, fmt.Errorf("unknown repo _type %q", typ)
	}
	return rc, nil
}

func toStrings(arr []hocon.Value) []string {
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := hocon.AsString(v); ok {
			out = append(out, s)
		}
	}
	return out
}
