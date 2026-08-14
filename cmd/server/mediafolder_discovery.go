package main

import (
	"encoding/xml"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

var versionFolderRegex = regexp.MustCompile(`^Version (\d+)$`)

// discoverMediaFolder finds RootsMagic's configured Media Folder by
// reading RootsMagicUser.xml directly -- the authoritative, per-user
// source RootsMagic itself uses. This isn't something a manually supplied
// flag can safely substitute for when writing: a wrong assumption about
// the Media Folder means writing a path that resolves correctly for this
// server but not for RootsMagic itself, silently breaking the link from
// RootsMagic's own point of view the next time someone opens the file
// there. See SCOPE.md's "Write support" section for the full reasoning,
// and why -write and -media-folder are therefore mutually exclusive.
//
// Two real locations are supported, both confirmed against actual
// installations/community reports (not assumed from general documentation
// conventions):
//
//   - Windows: %APPDATA%\RootsMagic\Version N\RootsMagicUser.xml --
//     confirmed directly against a real installation earlier in this
//     project's development.
//   - macOS: ~/RootsMagic/Version N/RootsMagicUser.xml -- based on
//     community reports (see SCOPE.md's "Write support" section for the
//     specific threads), not independently confirmed against a real Mac
//     the way the Windows location was. Treat this with a little more
//     caution until someone can verify it directly.
//
// If more than one "Version N" folder exists under either location
// (RootsMagic's own layout versions itself this way, so this happens
// whenever someone's used more than one RootsMagic version), the highest
// N is used: schema migrations are understood to be one-directional, so
// the highest version installed is presumed to be the one actually in
// current use. If the found configurations' Media Folder values disagree
// with each other, that's logged in detail -- which versions, which
// values -- but isn't treated as fatal; the highest version's value is
// used regardless.
//
// bypassOSCheck forces the macOS-style discovery path (a home-directory-
// relative location, unlike Windows's environment-variable-relative one)
// regardless of the actual runtime.GOOS. This exists specifically so
// write mode's Media Folder discovery can be exercised for real, end to
// end, from a development environment that's neither Windows nor macOS --
// os.UserHomeDir() returns a real, usable directory on any platform, so
// this isn't a fake/simulated path, it's the genuine macOS convention
// pointed at whatever this platform's actual home directory is. See the
// -bypass-os-check flag in main.go, deliberately undocumented in -h
// output: this is a development/testing aid, not a supported way to run
// write mode in production on an unsupported platform.
func discoverMediaFolder(bypassOSCheck bool) (string, error) {
	var rootsMagicDir string
	switch {
	case bypassOSCheck:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("couldn't determine the Media Folder: couldn't determine the home directory: %w", err)
		}
		rootsMagicDir = filepath.Join(home, "RootsMagic")
	case runtime.GOOS == "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", fmt.Errorf("couldn't determine the Media Folder: %%APPDATA%% is not set")
		}
		rootsMagicDir = filepath.Join(appData, "RootsMagic")
	case runtime.GOOS == "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("couldn't determine the Media Folder: couldn't determine the home directory: %w", err)
		}
		rootsMagicDir = filepath.Join(home, "RootsMagic")
	default:
		return "", fmt.Errorf("write mode requires reading RootsMagic's own configuration (RootsMagicUser.xml) to determine the Media Folder, which only exists on Windows and macOS -- not supported on %s", runtime.GOOS)
	}

	return discoverMediaFolderIn(rootsMagicDir)
}

// discoverMediaFolderIn does the actual "Version N" enumeration and
// conflict handling, shared identically across Windows, macOS, and the
// -bypass-os-check path -- only the base directory differs between them
// (see discoverMediaFolder), everything about interpreting what's inside
// it is the same regardless of platform.
func discoverMediaFolderIn(rootsMagicDir string) (string, error) {
	entries, err := os.ReadDir(rootsMagicDir)
	if err != nil {
		return "", fmt.Errorf("couldn't determine the Media Folder: reading %s: %w", rootsMagicDir, err)
	}

	type versionedFile struct {
		version int
		path    string
	}
	var found []versionedFile
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m := versionFolderRegex.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		xmlPath := filepath.Join(rootsMagicDir, e.Name(), "RootsMagicUser.xml")
		if _, statErr := os.Stat(xmlPath); statErr == nil {
			found = append(found, versionedFile{version: n, path: xmlPath})
		}
	}
	if len(found) == 0 {
		return "", fmt.Errorf("couldn't determine the Media Folder: no RootsMagicUser.xml found under %s", rootsMagicDir)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].version > found[j].version })

	type versionedMedia struct {
		version int
		media   string
	}
	var values []versionedMedia
	for _, f := range found {
		media, err := readMediaFolderFromXML(f.path)
		if err != nil {
			slog.Warn("couldn't read Media Folder config file, skipping it", "path", f.path, "error", err)
			continue
		}
		values = append(values, versionedMedia{version: f.version, media: media})
	}
	if len(values) == 0 {
		return "", fmt.Errorf("couldn't determine the Media Folder: found RootsMagicUser.xml file(s) under %s, but none could be read", rootsMagicDir)
	}

	chosen := values[0] // already sorted descending by version
	for _, v := range values[1:] {
		if v.media != chosen.media {
			slog.Warn("RootsMagic versions have different configured Media Folders -- using the newer version's value",
				"newerVersion", chosen.version, "newerMediaFolder", chosen.media,
				"olderVersion", v.version, "olderMediaFolder", v.media)
		}
	}

	if chosen.media == "" {
		return "", fmt.Errorf("couldn't determine the Media Folder: RootsMagic Version %d's configuration doesn't have one set (Folder Settings > Media Folder is empty)", chosen.version)
	}
	return chosen.media, nil
}

func readMediaFolderFromXML(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var cfg struct {
		Folders struct {
			Media string `xml:"Media"`
		} `xml:"Folders"`
	}
	if err := xml.Unmarshal(data, &cfg); err != nil {
		return "", err
	}
	return strings.TrimSpace(cfg.Folders.Media), nil
}
