package server

import (
	"io"
	"net/http"
	"os"
)

// writeBodyToFile streams the request body to dst (mirrors the PUT body sink).
func writeBodyToFile(r *http.Request, dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, r.Body); err != nil {
		return err
	}
	return f.Close()
}
