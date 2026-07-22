package rmdb

import (
	"fmt"
	"path/filepath"
	"strings"
)

// MediaFolderConfig holds the filesystem locations needed to resolve a
// RootsMagic MultimediaTable path to an absolute file on disk.
type MediaFolderConfig struct {
	// DatabaseDir is the directory containing the RootsMagic database file
	// itself -- used to resolve the "*" MediaPath symbol, and as the
	// default base for MediaPath values that have no leading symbol at all
	// (RootsMagic's common default layout: media stored in a folder next
	// to the database).
	DatabaseDir string
	// HomeDir is the resolving user's home directory -- used for "~".
	HomeDir string
	// MediaFolder is the "Media Folder" configured in RootsMagic's own
	// Folder Settings window -- used for "?". This isn't stored anywhere
	// in the RootsMagic file itself (confirmed by inspecting ConfigTable's
	// DataRec, which is plain XML, not an opaque format -- it just
	// genuinely doesn't contain this setting; it's almost certainly a
	// local, per-installation preference, e.g. in the Windows Registry).
	// So it must be supplied explicitly -- the -media-folder flag -- if
	// any MediaPath in the file uses "?". Left empty, resolving a "?" path
	// fails with a clear error. See SCOPE.md's "Multimedia" section.
	MediaFolder string
}

// LooksLikeExternalReference reports whether a RootsMagic MediaPath value
// appears to already be a URL/URI (e.g. a web-hint or record-match
// reference from an online search provider) rather than a local file path.
// This is a real, observed pattern -- not hypothetical: RootsMagic files
// built from online search integrations can have MultimediaTable rows like
// MediaPath = `http:\search.example.com{0}\transcript?id=...`, MediaFile =
// `12345` (apparently meant to be substituted into the "{0}" placeholder).
// The data dictionary doesn't document this convention (MultimediaTable.URL
// is documented as "Not implemented" and is empty in practice), and the
// exact substitution rule isn't guessable with confidence -- guessing wrong
// would produce a broken link presented as if it worked, which is worse
// than not trying. So this server only detects the pattern, to skip trying
// to open such a value as a local file; it doesn't attempt to reconstruct
// a working URL from it. See SCOPE.md's "Multimedia" section.
func LooksLikeExternalReference(mediaPath string) bool {
	normalized := strings.TrimLeft(mediaPath, "?~*")
	i := strings.IndexAny(normalized, ":/\\")
	if i <= 0 || normalized[i] != ':' {
		return false
	}
	scheme := normalized[:i]
	if len(scheme) < 2 {
		// A single letter before ":" is a Windows drive letter (C:\...),
		// not a URI scheme -- real schemes are always 2+ characters.
		return false
	}
	for _, r := range scheme {
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		isOther := (r >= '0' && r <= '9') || r == '+' || r == '-' || r == '.'
		if !isLetter && !isOther {
			return false
		}
	}
	return true
}

// ResolveMediaPath resolves a RootsMagic MultimediaTable (MediaPath,
// MediaFile) pair to an absolute filesystem path, per the encoding
// documented in the RootsMagic data dictionary: the first character of
// MediaPath may be a symbol selecting the base directory:
//
//	'?' the configured Media Folder (cfg.MediaFolder)
//	'~' the user's home directory (cfg.HomeDir)
//	'*' the folder containing the RootsMagic database (cfg.DatabaseDir)
//
// A MediaPath with none of those leading symbols is resolved relative to
// cfg.DatabaseDir (RootsMagic's common default), unless it's already an
// absolute path (Unix "/..." or Windows "C:\..."), in which case it's used
// as-is.
//
// Backslashes are normalized to the host OS's separator, since RootsMagic
// databases are frequently created on Windows and may end up served from a
// different OS -- though a Windows *absolute* path (a drive letter) can't
// be resolved on a non-Windows host regardless; see SCOPE.md.
func ResolveMediaPath(mediaPath, mediaFile string, cfg MediaFolderConfig) (string, error) {
	normalized := strings.ReplaceAll(mediaPath, `\`, "/")
	file := filepath.FromSlash(strings.ReplaceAll(mediaFile, `\`, "/"))

	if isAbsoluteish(normalized) &&
		!strings.HasPrefix(normalized, "?") && !strings.HasPrefix(normalized, "~") && !strings.HasPrefix(normalized, "*") {
		return filepath.Join(filepath.FromSlash(normalized), file), nil
	}

	var base, rest string
	switch {
	case strings.HasPrefix(normalized, "?"):
		if cfg.MediaFolder == "" {
			return "", fmt.Errorf("media path %q uses the RootsMagic Media Folder symbol ('?') but no -media-folder was configured", mediaPath)
		}
		base, rest = cfg.MediaFolder, strings.TrimPrefix(normalized, "?")
	case strings.HasPrefix(normalized, "~"):
		if cfg.HomeDir == "" {
			return "", fmt.Errorf("media path %q uses the home directory symbol ('~') but no home directory is available", mediaPath)
		}
		base, rest = cfg.HomeDir, strings.TrimPrefix(normalized, "~")
	case strings.HasPrefix(normalized, "*"):
		base, rest = cfg.DatabaseDir, strings.TrimPrefix(normalized, "*")
	default:
		base, rest = cfg.DatabaseDir, normalized
	}

	rest = strings.TrimPrefix(rest, "/")
	full := filepath.Join(filepath.FromSlash(base), filepath.FromSlash(rest))
	return filepath.Join(full, file), nil
}

// isAbsoluteish reports whether a slash-normalized path looks like an
// absolute path on either Unix ("/...") or Windows ("C:/...").
func isAbsoluteish(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	if len(p) >= 3 && p[1] == ':' && p[2] == '/' {
		c := p[0]
		return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	}
	return false
}
