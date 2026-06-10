package hocon

import (
	"os"
	"testing"
)

const sample = `
locus.server.app: {
  proxyServerAddresses: [ "10.101.0.0/16", "127.0.0.0/8" ]
  anonymousSubnets: [
    "127.0.0.0/16",
    "172.24.0.0/16",
    "10.101.0.0/24"
    "172.25.0.0/16"
  ]
  dataDirectory: "data"
  s3: {
    accessKey: "AK123",
    secretKey: "sec/with+slashes"
  }
  repos: [
    {
      _type: "multiplexer"
      name: "all"
      repos: ["maven2", "releases"]
      repoForWrites: "releases"
      generatedChecksums: [all]
    }
    {
      _type: "url"
      name: "maven2"
      url: "https://repo1.maven.org/maven2"
      generatedChecksums: [all]
    }
  ]
  port: 7001
  realm: "Accur8 Repo"
  unquotedUrl: https://example.com/a//b
}
`

func TestParseSample(t *testing.T) {
	root, err := Parse(sample)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	app := Lookup(root, "locus.server.app")
	if app == nil {
		t.Fatal("locus.server.app missing")
	}
	if v := Lookup(root, "locus.server.app.port"); v != "7001" {
		t.Errorf("port = %q want 7001", v)
	}
	if v := Lookup(root, "locus.server.app.dataDirectory"); v != "data" {
		t.Errorf("dataDirectory = %q want data", v)
	}
	if v := Lookup(root, "locus.server.app.realm"); v != "Accur8 Repo" {
		t.Errorf("realm = %q want 'Accur8 Repo' (no quotes)", v)
	}
	if v := Lookup(root, "locus.server.app.s3.secretKey"); v != "sec/with+slashes" {
		t.Errorf("secretKey = %q", v)
	}
	// unquoted URL with // must survive intact
	if v := Lookup(root, "locus.server.app.unquotedUrl"); v != "https://example.com/a//b" {
		t.Errorf("unquotedUrl = %q", v)
	}
	anon, ok := AsArray(Lookup(root, "locus.server.app.anonymousSubnets"))
	if !ok || len(anon) != 4 {
		t.Fatalf("anonymousSubnets len = %d want 4 (newline-separated)", len(anon))
	}
	if anon[2] != "10.101.0.0/24" {
		t.Errorf("anon[2] = %q", anon[2])
	}
	repos, ok := AsArray(Lookup(root, "locus.server.app.repos"))
	if !ok || len(repos) != 2 {
		t.Fatalf("repos len = %d want 2", len(repos))
	}
	mux, _ := AsObject(repos[0])
	if mux["_type"] != "multiplexer" || mux["name"] != "all" {
		t.Errorf("repo[0] = %v", mux)
	}
	inner, ok := AsArray(mux["repos"])
	if !ok || len(inner) != 2 || inner[0] != "maven2" {
		t.Errorf("mux.repos = %v", mux["repos"])
	}
	gc, _ := AsArray(mux["generatedChecksums"])
	if len(gc) != 1 || gc[0] != "all" {
		t.Errorf("generatedChecksums = %v want [all]", mux["generatedChecksums"])
	}
}

// TestParseProdConfig parses the real config if present (gitignored).
func TestParseProdConfig(t *testing.T) {
	path := "../../../config/config.hocon"
	if _, err := os.Stat(path); err != nil {
		t.Skip("prod config not present")
	}
	root, err := ParseFile(path)
	if err != nil {
		t.Fatalf("parse prod config: %v", err)
	}
	repos, ok := AsArray(Lookup(root, "locus.server.app.repos"))
	if !ok || len(repos) == 0 {
		t.Fatalf("prod repos missing")
	}
	if v := Lookup(root, "locus.server.app.port"); v != "7001" {
		t.Errorf("prod port = %q", v)
	}
}
