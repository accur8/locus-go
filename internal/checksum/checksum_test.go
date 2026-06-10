package checksum

import "testing"

func TestDigestKnown(t *testing.T) {
	// echo -n "abc"
	if got := HexString(Md5.DigestString("abc")); got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Errorf("md5(abc) = %q", got)
	}
	if got := HexString(Sha1.DigestString("abc")); got != "a9993e364706816aba3e25717850c26c9cd0d89d" {
		t.Errorf("sha1(abc) = %q", got)
	}
	if got := HexString(Sha256.DigestString("abc")); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("sha256(abc) = %q", got)
	}
}

func TestScrub(t *testing.T) {
	cases := map[string]string{
		"ABCDEF":               "abcdef",
		"  abc  ":              "abc",
		"abc  somefile.jar":    "abc",
		"04895AD6E0BBAC1BAFBF": "04895ad6e0bbac1bafbf",
	}
	for in, want := range cases {
		if got := Scrub(in); got != want {
			t.Errorf("Scrub(%q) = %q want %q", in, got, want)
		}
	}
}

func TestIsChecksumExt(t *testing.T) {
	for _, e := range []string{"md5", "MD5", "sha1", "sha256"} {
		if !IsChecksumExt(e) {
			t.Errorf("%q should be checksum ext", e)
		}
	}
	for _, e := range []string{"jar", "pom", "xml", ""} {
		if IsChecksumExt(e) {
			t.Errorf("%q should not be checksum ext", e)
		}
	}
}

func TestSets(t *testing.T) {
	if len(Validators) != 2 || len(ResponseHeaders) != 2 || len(All) != 3 {
		t.Errorf("set sizes wrong: %d %d %d", len(Validators), len(ResponseHeaders), len(All))
	}
	if Sha256.IncludeInRespHeader {
		t.Errorf("sha256 must not be in response headers")
	}
}
