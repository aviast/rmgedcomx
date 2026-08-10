package rmdb

import (
	"errors"
	"fmt"
	"strings"
)

// ErrPathNotUnderMediaFolder is returned by encodeMediaPath when the given
// real path isn't actually under the configured Media Folder -- see its
// own doc comment for why this server refuses to write anything else.
var ErrPathNotUnderMediaFolder = errors.New("path is not under the Media Folder")

// encodeMediaPath converts a real, absolute filesystem path into
// RootsMagic's "?"-relative MediaPath/MediaFile encoding, anchored at the
// given Media Folder -- the reverse of ResolveMediaPath's "?" case (see
// mediapath.go). See SCOPE.md's "Write support" section for why "?" is
// the only encoding this server will ever write, never "*", "~", or an
// absolute path: it's the only one whose meaning doesn't depend on which
// machine either side of a write happens to be running on.
//
// Deliberately implemented with explicit backslash normalization and
// manual string manipulation rather than the path/filepath package, which
// behaves according to the *build* platform, not a runtime-selectable
// one -- this needs to work with Windows path syntax specifically
// (backslashes, drive letters, case-insensitive) regardless of which OS
// this code is compiled for, the same reasoning already applied to
// ResolveMediaPath and collectionid.fileStem.
//
// Returns ErrPathNotUnderMediaFolder if realPath isn't actually under
// mediaFolder -- writing anything else would break the one guarantee this
// whole mechanism exists to provide.
func encodeMediaPath(mediaFolder, realPath string) (mediaPath, mediaFile string, err error) {
	folder := strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(mediaFolder), "/", `\`), `\`)
	path := strings.ReplaceAll(strings.TrimSpace(realPath), "/", `\`)

	if folder == "" {
		return "", "", fmt.Errorf("no Media Folder configured")
	}

	// Case-insensitive prefix match -- Windows paths are case-insensitive
	// -- and the character right after the matched prefix must be a
	// separator (or the whole path must end there), so "C:\tmp2\x.jpg"
	// isn't incorrectly treated as being under "C:\tmp".
	if len(path) < len(folder) || !strings.EqualFold(path[:len(folder)], folder) {
		return "", "", fmt.Errorf("%w: %q is not under %q", ErrPathNotUnderMediaFolder, realPath, mediaFolder)
	}
	rest := path[len(folder):]
	if rest == "" {
		return "", "", fmt.Errorf("%w: %q is the Media Folder itself, not a file inside it", ErrPathNotUnderMediaFolder, realPath)
	}
	if rest[0] != '\\' {
		return "", "", fmt.Errorf("%w: %q is not under %q", ErrPathNotUnderMediaFolder, realPath, mediaFolder)
	}
	rest = rest[1:] // strip the separator right after the Media Folder

	idx := strings.LastIndex(rest, `\`)
	if idx < 0 {
		// The file sits directly in the Media Folder, no subdirectory.
		return "?", rest, nil
	}
	return `?\` + rest[:idx], rest[idx+1:], nil
}
