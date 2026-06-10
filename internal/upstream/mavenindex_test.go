package upstream

import (
	"testing"

	"github.com/accur8/locus-go/internal/uri"
)

const idxHTML = `<!DOCTYPE html><html><body><main><pre id="contents">
<a href="../">../</a>
<a href="10.0/" title="10.0/">10.0/</a>                    2011-09-28 01:28         -
<a href="guava-19.0.pom" title="guava-19.0.pom">guava-19.0.pom</a>          2015-12-09 20:58      6792
<a href="maven-metadata.xml" title="maven-metadata.xml">maven-metadata.xml</a>  2017-03-03 04:31       375
</pre></main></body></html>`

func TestParseMavenIndex(t *testing.T) {
	base := uri.MustParse("https://repo1.maven.org/maven2/com/google/guava/guava")
	entries, err := ParseMavenIndex(base, idxHTML, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("count = %d want 3 (.. skipped)", len(entries))
	}
	dir := entries[0]
	if dir.Name != "10.0" || !dir.IsDirectory || dir.Size != nil {
		t.Errorf("dir entry = %+v", dir)
	}
	if dir.LastModified == nil || dir.LastModified.String() != "2011-09-28T01:28" {
		t.Errorf("dir lastModified wrong")
	}
	if dir.DirectURL == nil || dir.DirectURL.String() != "https://repo1.maven.org/maven2/com/google/guava/guava/10.0" {
		t.Errorf("dir url = %v", dir.DirectURL)
	}
	pom := entries[1]
	if pom.Name != "guava-19.0.pom" || pom.IsDirectory || pom.Size == nil || *pom.Size != 6792 {
		t.Errorf("pom entry = %+v size=%v", pom, pom.Size)
	}
}
