// Package media stores uploaded images.
//
// The rule that matters: the name a browser sends is never used as a path. It
// is kept as file_name metadata so the library can show what an editor
// uploaded, and the bytes are written under a name generated here. A filename
// of ../../.env is then just an unusual label rather than a write outside the
// media directory.
package media

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"cms-backend/internal/apierr"
	"cms-backend/internal/config"
)

// allowed lists the types the marketing site renders. SVG is included
// deliberately (the existing artwork is SVG) and is served from the same
// origin as the dashboard, so uploads are restricted to signed-in users.
var allowed = map[string]string{
	"image/png":     ".png",
	"image/jpeg":    ".jpg",
	"image/webp":    ".webp",
	"image/gif":     ".gif",
	"image/svg+xml": ".svg",
	"image/avif":    ".avif",
}

type Stored struct {
	URL          string
	AbsolutePath string
	ByteSize     int64
	MimeType     string
}

type Storage struct{ cfg *config.Config }

func New(cfg *config.Config) *Storage { return &Storage{cfg: cfg} }

func (s *Storage) Store(fh *multipart.FileHeader) (*Stored, error) {
	mimeType := fh.Header.Get("Content-Type")
	ext, ok := allowed[mimeType]
	if !ok {
		shown := mimeType
		if shown == "" {
			shown = "of unknown type"
		}
		return nil, apierr.Validation(map[string]string{"file": "Unsupported file type."},
			"Upload a PNG, JPEG, WebP, GIF, AVIF or SVG. That file is "+shown+".")
	}
	if fh.Size <= 0 {
		return nil, apierr.Validation(map[string]string{"file": "That file is empty."}, "")
	}
	if fh.Size > s.cfg.MediaMaxBytes {
		return nil, apierr.Validation(map[string]string{
			"file": fmt.Sprintf("Too large. The limit is %dMB.", s.cfg.MediaMaxBytes/1024/1024),
		}, "")
	}

	src, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	// The declared type is the browser's word. For the raster formats, check
	// the bytes agree so a script cannot be uploaded as image/png.
	head := make([]byte, 512)
	n, _ := io.ReadFull(src, head)
	head = head[:n]
	if sniffed := http.DetectContentType(head); mimeType != "image/svg+xml" && mimeType != "image/avif" && sniffed != mimeType {
		return nil, apierr.Validation(map[string]string{"file": "The file's contents do not match its type."}, "")
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	folder := fmt.Sprintf("%04d/%02d", now.Year(), int(now.Month()))
	name := uuid.NewString() + ext

	dir := filepath.Join(s.cfg.MediaDir, filepath.FromSlash(folder))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	abs := filepath.Join(dir, name)

	dst, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(abs)
		return nil, err
	}
	if err := dst.Close(); err != nil {
		os.Remove(abs)
		return nil, err
	}

	return &Stored{
		URL:          s.cfg.MediaPublicPath + "/" + folder + "/" + name,
		AbsolutePath: abs,
		ByteSize:     fh.Size,
		MimeType:     mimeType,
	}, nil
}

// Discard removes a file written moments ago when its database row could not
// be made. Failing the request over a leftover would report the wrong problem.
func (s *Storage) Discard(path string) { _ = os.Remove(path) }
