package rmdb

import (
	"errors"
	"testing"
)

func TestEncodeMediaPath(t *testing.T) {
	cases := []struct {
		name          string
		mediaFolder   string
		realPath      string
		wantMediaPath string
		wantMediaFile string
	}{
		{
			name:          "file in a subdirectory",
			mediaFolder:   `C:\tmp`,
			realPath:      `C:\tmp\royal92\marriage-of-queen-victoria.jpg`,
			wantMediaPath: `?\royal92`,
			wantMediaFile: `marriage-of-queen-victoria.jpg`,
		},
		{
			name:          "file directly in the Media Folder, no subdirectory",
			mediaFolder:   `C:\tmp`,
			realPath:      `C:\tmp\photo.jpg`,
			wantMediaPath: `?`,
			wantMediaFile: `photo.jpg`,
		},
		{
			name:          "nested subdirectories",
			mediaFolder:   `C:\Users\micha\Genealogy Media`,
			realPath:      `C:\Users\micha\Genealogy Media\Photographs\1970s\wedding.jpg`,
			wantMediaPath: `?\Photographs\1970s`,
			wantMediaFile: `wedding.jpg`,
		},
		{
			name:          "case-insensitive match (Windows paths)",
			mediaFolder:   `C:\Tmp`,
			realPath:      `c:\tmp\royal92\marriage-of-queen-victoria.jpg`,
			wantMediaPath: `?\royal92`,
			wantMediaFile: `marriage-of-queen-victoria.jpg`,
		},
		{
			name:          "forward slashes normalized",
			mediaFolder:   `C:\tmp`,
			realPath:      `C:/tmp/royal92/marriage-of-queen-victoria.jpg`,
			wantMediaPath: `?\royal92`,
			wantMediaFile: `marriage-of-queen-victoria.jpg`,
		},
		{
			name:          "trailing separator on Media Folder is tolerated",
			mediaFolder:   `C:\tmp\`,
			realPath:      `C:\tmp\royal92\marriage-of-queen-victoria.jpg`,
			wantMediaPath: `?\royal92`,
			wantMediaFile: `marriage-of-queen-victoria.jpg`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotPath, gotFile, err := encodeMediaPath(c.mediaFolder, c.realPath)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotPath != c.wantMediaPath {
				t.Errorf("mediaPath = %q, want %q", gotPath, c.wantMediaPath)
			}
			if gotFile != c.wantMediaFile {
				t.Errorf("mediaFile = %q, want %q", gotFile, c.wantMediaFile)
			}
		})
	}
}

func TestEncodeMediaPathRejectsPathsOutsideMediaFolder(t *testing.T) {
	cases := []struct {
		name        string
		mediaFolder string
		realPath    string
	}{
		{
			name:        "different drive/directory entirely",
			mediaFolder: `C:\tmp`,
			realPath:    `D:\Photos\wedding.jpg`,
		},
		{
			name:        "sibling directory with matching prefix string",
			mediaFolder: `C:\tmp`,
			realPath:    `C:\tmp2\royal92\wedding.jpg`,
		},
		{
			name:        "the Media Folder itself, not a file inside it",
			mediaFolder: `C:\tmp`,
			realPath:    `C:\tmp`,
		},
		{
			name:        "no Media Folder configured",
			mediaFolder: "",
			realPath:    `C:\tmp\royal92\wedding.jpg`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := encodeMediaPath(c.mediaFolder, c.realPath)
			if err == nil {
				t.Fatal("expected an error, got none")
			}
			if c.mediaFolder != "" && !errors.Is(err, ErrPathNotUnderMediaFolder) {
				t.Errorf("expected ErrPathNotUnderMediaFolder, got: %v", err)
			}
		})
	}
}
