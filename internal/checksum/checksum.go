// Package checksum mirrors a8.locus.ChecksumHandler: md5/sha1/sha256 digesting,
// validation, and the response-header / validator / generator sets.
package checksum

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"hash"
	"io"
	"os"
	"strings"
)

// Kind is one checksum algorithm.
type Kind struct {
	Ext                 string // e.g. "sha256"
	IncludeInRespHeader bool
	newHash             func() hash.Hash
}

var (
	Sha256 = Kind{Ext: "sha256", IncludeInRespHeader: false, newHash: sha256.New}
	Md5    = Kind{Ext: "md5", IncludeInRespHeader: true, newHash: md5.New}
	Sha1   = Kind{Ext: "sha1", IncludeInRespHeader: true, newHash: sha1.New}
)

// Validators are the checksums repos publish and we validate against
// (deliberately NOT sha256). Mirrors ChecksumHandler.validators.
var Validators = []Kind{Md5, Sha1}

// ResponseHeaders are emitted as x-checksum-* on GET. Mirrors responseHeaders.
var ResponseHeaders = []Kind{Md5, Sha1}

// All is validators ++ [sha256]. Mirrors ChecksumHandler.all.
var All = []Kind{Md5, Sha1, Sha256}

// ExtensionLc is the lower-cased extension.
func (k Kind) ExtensionLc() string { return strings.ToLower(k.Ext) }

// DigestReader digests an io.Reader.
func (k Kind) DigestReader(r io.Reader) ([]byte, error) {
	h := k.newHash()
	if _, err := io.Copy(h, r); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// DigestFile digests a file's contents.
func (k Kind) DigestFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return k.DigestReader(f)
}

// DigestString digests a UTF-8 string.
func (k Kind) DigestString(s string) []byte {
	h := k.newHash()
	io.WriteString(h, s)
	return h.Sum(nil)
}

// HexString is lower-case hex (matches commons-codec Hex.encodeHexString).
func HexString(b []byte) string { return hex.EncodeToString(b) }

// Base64String matches DigestResults.asBase64String.
func Base64String(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// IsChecksumExt reports whether the (any-case) extension is a checksum ext.
func IsChecksumExt(ext string) bool {
	e := strings.ToLower(ext)
	for _, k := range All {
		if k.Ext == e {
			return true
		}
	}
	return false
}

// Scrub mirrors ChecksumHandler.scrubChecksum: first whitespace-delimited token,
// lower-cased ("" when none).
func Scrub(s string) string {
	for _, tok := range strings.Split(s, " ") {
		t := strings.TrimSpace(tok)
		if len(t) > 0 {
			return strings.ToLower(t)
		}
	}
	return ""
}
