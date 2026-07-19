package api

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"runtime"
	"strings"
	"testing"
)

func TestStreamWireFormat(t *testing.T) {
	fileBody := "hello attachment body"
	reader, contentType := Stream("file", "report.txt", "text/plain; charset=utf-8",
		strings.NewReader(fileBody), map[string]string{
			"owner_id":   "own-1",
			"owner_type": "artifact",
		})

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("content type %q not parseable: %v", contentType, err)
	}
	if mediaType != "multipart/form-data" || params["boundary"] == "" {
		t.Fatalf("wrong content type: %q", contentType)
	}

	mr := multipart.NewReader(reader, params["boundary"])
	fields := map[string]string{}
	var fileName, filePartCT, fileContent string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		data, _ := io.ReadAll(part)
		switch part.FormName() {
		case "file":
			fileName = part.FileName()
			filePartCT = part.Header.Get("Content-Type")
			fileContent = string(data)
		default:
			fields[part.FormName()] = string(data)
		}
	}

	if fields["owner_id"] != "own-1" || fields["owner_type"] != "artifact" {
		t.Errorf("text fields wrong: %+v", fields)
	}
	if fileName != "report.txt" {
		t.Errorf("filename = %q, want report.txt", fileName)
	}
	if filePartCT != "text/plain; charset=utf-8" {
		t.Errorf("file part content-type = %q", filePartCT)
	}
	if fileContent != fileBody {
		t.Errorf("file content = %q, want %q", fileContent, fileBody)
	}
}

// TestStreamPropagatesReadError: an error from the source reader surfaces on the
// body reader via CloseWithError rather than hanging.
func TestStreamPropagatesReadError(t *testing.T) {
	reader, _ := Stream("file", "x", "", errReader{}, nil)
	_, err := io.ReadAll(reader)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected the source error to propagate, got %v", err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

// TestStreamBoundedMemory streams a large synthetic file and asserts heap growth
// stays far below the payload size — proving the body is not buffered.
func TestStreamBoundedMemory(t *testing.T) {
	const size = 64 << 20 // 64 MiB
	src := io.LimitReader(zeroReader{}, size)

	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	reader, _ := Stream("file", "big.bin", "application/octet-stream", src, nil)
	n, err := io.Copy(io.Discard, reader) // small internal buffer, like the transport
	if err != nil {
		t.Fatalf("stream copy: %v", err)
	}
	if n < size {
		t.Fatalf("streamed %d bytes, want >= %d", n, size)
	}

	var after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&after)
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if growth > size/4 {
		t.Errorf("heap grew by %d bytes streaming %d — body appears buffered", growth, size)
	}
}

type zeroReader struct{}

func (zeroReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 0
	}
	return len(b), nil
}
