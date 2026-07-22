package rmdb

import "testing"

func TestResolveMediaPath(t *testing.T) {
	cfg := MediaFolderConfig{
		DatabaseDir: "/data/trees",
		HomeDir:     "/home/alice",
		MediaFolder: "/mnt/rm-media",
	}

	cases := []struct {
		name      string
		mediaPath string
		mediaFile string
		want      string
	}{
		{"media folder symbol", "?Photos", "portrait.jpg", "/mnt/rm-media/Photos/portrait.jpg"},
		{"home dir symbol", "~/Genealogy/Scans", "will.pdf", "/home/alice/Genealogy/Scans/will.pdf"},
		{"db dir symbol", "*Media", "cert.jpg", "/data/trees/Media/cert.jpg"},
		{"no symbol, relative", "Media/Photos", "cert.jpg", "/data/trees/Media/Photos/cert.jpg"},
		{"unix absolute passthrough", "/srv/archive/photos", "old.png", "/srv/archive/photos/old.png"},
		{"windows-style backslashes under db dir symbol", `*Media\Photos`, "img.jpg", "/data/trees/Media/Photos/img.jpg"},
		{"bare symbol, no subfolder", "*", "cert.jpg", "/data/trees/cert.jpg"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveMediaPath(c.mediaPath, c.mediaFile, cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("ResolveMediaPath(%q, %q) = %q, want %q", c.mediaPath, c.mediaFile, got, c.want)
			}
		})
	}
}

func TestLooksLikeExternalReference(t *testing.T) {
	cases := []struct {
		name      string
		mediaPath string
		want      bool
	}{
		{"real observed findmypast pattern", `http:\search.findmypast.com{0}\transcript?id=BMD\B\1950\3\AZ\000483`, true},
		{"https scheme", "https://example.com/cert.jpg", true},
		{"local file, no symbol", "Media/Photos", false},
		{"db-dir symbol", "*Media/Photos", false},
		{"media-folder symbol", "?Photos", false},
		{"home-dir symbol", "~/Genealogy", false},
		{"unix absolute", "/srv/archive/photos", false},
		{"windows absolute", `C:\Users\alice\Photos`, false},
		{"bare filename", "cert.jpg", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := LooksLikeExternalReference(c.mediaPath)
			if got != c.want {
				t.Errorf("LooksLikeExternalReference(%q) = %v, want %v", c.mediaPath, got, c.want)
			}
		})
	}
}

func TestResolveMediaPathMissingConfig(t *testing.T) {
	// "?" with no -media-folder configured must fail clearly, not silently
	// resolve to a wrong or empty path.
	_, err := ResolveMediaPath("?Photos", "portrait.jpg", MediaFolderConfig{DatabaseDir: "/data/trees"})
	if err == nil {
		t.Fatal("expected an error when MediaFolder is unconfigured, got nil")
	}
}
