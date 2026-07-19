package api

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
)

// Stream builds a streamed multipart/form-data body. It returns a reader that
// yields the encoded body and the request Content-Type (carrying the boundary)
// to send with it. The file part is streamed from r through an io.Pipe +
// multipart.Writer running in a goroutine, so process memory stays bounded
// regardless of file size. Any extra text fields are written before the file
// part. Encoding errors are propagated to the reader via CloseWithError.
//
// It is owner-agnostic so both team-scoped and (future) artifact-scoped
// attachment uploads can reuse it.
func Stream(fileField, filename, fileContentType string, r io.Reader, fields map[string]string) (io.Reader, string) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	contentType := mw.FormDataContentType() // boundary is fixed at NewWriter

	go func() {
		for k, v := range fields {
			if err := mw.WriteField(k, v); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
		}
		part, err := mw.CreatePart(filePartHeader(fileField, filename, fileContentType))
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, r); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if err := mw.Close(); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.Close()
	}()

	return pr, contentType
}

// filePartHeader builds the MIME header for the file part, setting an explicit
// Content-Type when one was detected/overridden (multipart.CreateFormFile would
// otherwise hardcode application/octet-stream).
func filePartHeader(field, filename, contentType string) textproto.MIMEHeader {
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
		escapeQuotes(field), escapeQuotes(filename)))
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return h
}

// escapeQuotes mirrors mime/multipart's private quoteEscaper for header values.
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string { return quoteEscaper.Replace(s) }
