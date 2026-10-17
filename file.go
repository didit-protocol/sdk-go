package didit

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// File is a file to upload in a multipart request: an image, a document or a
// CSV. Create one with FileFromPath, FileFromBytes or FileFromReader.
//
// The content type is, in order: the one set with WithContentType, a fixed
// table for the image and document types the API accepts (by file name
// extension), the type sniffed from the first bytes, and
// application/octet-stream.
type File struct {
	path        string
	data        []byte
	reader      io.Reader
	name        string
	contentType string
}

// FileFromPath returns a File read from path when the request is sent. The
// file name sent is the base name of path.
func FileFromPath(path string) File {
	return File{path: path, name: filepath.Base(path)}
}

// FileFromBytes returns a File holding data, sent with the given file name.
func FileFromBytes(name string, data []byte) File {
	return File{data: data, name: name}
}

// FileFromReader returns a File read from r when the request is sent, with
// the given file name. The SDK reads r once and keeps the bytes in memory so
// that a retried request sends the same content.
func FileFromReader(r io.Reader, name string) File {
	return File{reader: r, name: name}
}

// WithName returns a copy of f sent with the given file name.
func (f File) WithName(name string) File {
	f.name = name
	return f
}

// WithContentType returns a copy of f sent with the given content type.
func (f File) WithContentType(contentType string) File {
	f.contentType = contentType
	return f
}

// Name returns the file name sent with the file.
func (f File) Name() string { return f.name }

// IsZero reports whether f was never set.
func (f File) IsZero() bool {
	return f.path == "" && f.data == nil && f.reader == nil
}

// load returns the file content and its content type.
func (f File) load() ([]byte, string, error) {
	var data []byte
	switch {
	case f.data != nil:
		data = f.data
	case f.reader != nil:
		b, err := io.ReadAll(f.reader)
		if err != nil {
			return nil, "", fmt.Errorf("reading file %q: %w", f.name, err)
		}
		data = b
	case f.path != "":
		b, err := os.ReadFile(f.path)
		if err != nil {
			return nil, "", fmt.Errorf("reading file: %w", err)
		}
		data = b
	default:
		return nil, "", fmt.Errorf("file %q has no content", f.name)
	}
	return data, f.detectContentType(data), nil
}

// contentTypes covers the upload types of the API, so that the result does
// not depend on the host's MIME database.
var contentTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
	".bmp":  "image/bmp",
	".tif":  "image/tiff",
	".tiff": "image/tiff",
	".heic": "image/heic",
	".heif": "image/heif",
	".avif": "image/avif",
	".pdf":  "application/pdf",
	".csv":  "text/csv",
	".json": "application/json",
	".txt":  "text/plain",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
}

func (f File) detectContentType(data []byte) string {
	if f.contentType != "" {
		return f.contentType
	}
	ext := strings.ToLower(filepath.Ext(f.name))
	if ct, ok := contentTypes[ext]; ok {
		return ct
	}
	if len(data) > 0 {
		if ct := http.DetectContentType(data); ct != "application/octet-stream" && !strings.HasPrefix(ct, "text/plain") {
			return ct
		}
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// readerOf returns a reader over b; it exists so tests can wrap it.
func readerOf(b []byte) io.Reader { return bytes.NewReader(b) }
