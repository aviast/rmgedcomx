package gedcomx

import (
	"mime"
	"path/filepath"
	"strings"
)

// mediaTypeFallback covers the extensions actually observed in real
// RootsMagic MultimediaTable data (photos, scanned certificates, PDFs,
// and the occasional HTML/Word document saved from a web-hint). It's
// checked before mime.TypeByExtension, which depends on the deployment
// environment having a populated /etc/mime.types (or equivalent) --
// fine on a typical Linux/macOS dev machine, not guaranteed on a minimal
// container image. This keeps mediaType resolution consistent regardless.
var mediaTypeFallback = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".bmp":  "image/bmp",
	".tif":  "image/tiff",
	".tiff": "image/tiff",
	".webp": "image/webp",
	".heic": "image/heic",
	".pdf":  "application/pdf",
	".doc":  "application/msword",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".htm":  "text/html",
	".html": "text/html",
	".txt":  "text/plain",
	".mp3":  "audio/mpeg",
	".wav":  "audio/vnd.wave",
	".mp4":  "video/mp4",
	".mov":  "video/quicktime",
	".avi":  "video/x-msvideo",
}

// MediaTypeForFilename returns a best-effort MIME type for a filename
// based on its extension, or "" if none could be determined.
func MediaTypeForFilename(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return ""
	}
	if t, ok := mediaTypeFallback[ext]; ok {
		return t
	}
	t := mime.TypeByExtension(ext)
	if t == "" {
		return ""
	}
	// mime.TypeByExtension often appends "; charset=..." for text types;
	// GEDCOM X's mediaType is meant as a plain MIME type hint.
	if i := strings.IndexByte(t, ';'); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	return t
}
