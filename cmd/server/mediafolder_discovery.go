package main

import (
	"encoding/xml"
	"fmt"
	"log"
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
// reading RootsMagicUser.xml directly -- the authoritative, per-Windows-
// user source RootsMagic itself uses. This isn't something a manually
// supplied flag can safely substitute for when writing: a wrong
// assumption about the Media Folder means writing a path that resolves
// correctly for this server but not for RootsMagic itself, silently
// breaking the link from RootsMagic's own point of view the next time
// someone opens the file there. See SCOPE.md's "Write support" section
// for the full reasoning, and why -write and -media-folder are therefore
// mutually exclusive.
//
// Only works on Windows, where RootsMagic actually runs, and where
// %APPDATA%\RootsMagic\Version N\RootsMagicUser.xml is the confirmed,
// real location -- confirmed directly against a real installation earlier
// in this project's development, not assumed from documentation.
//
// If more than one "Version N" folder exists (RootsMagic's own AppData
// layout versions itself, so this happens whenever someone's used more
// than one RootsMagic version), the highest N is used: schema migrations
// are understood to be one-directional, so the highest version installed
// is presumed to be the one actually in current use. If the found
// configurations' Media Folder values disagree with each other, that's
// logged in detail -- which versions, which values -- but isn't treated
// as fatal; the highest version's value is used regardless.
func discoverMediaFolder() (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("write mode requires reading RootsMagic's own configuration (RootsMagicUser.xml) to determine the Media Folder, which only exists on Windows -- not supported on %s yet", runtime.GOOS)
	}

	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("couldn't determine the Media Folder: %%APPDATA%% is not set")
	}

	rootsMagicDir := filepath.Join(appData, "RootsMagic")
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
			log.Printf("warning: couldn't read %s (%v), skipping it", f.path, err)
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
			log.Printf("warning: RootsMagic Version %d and Version %d have different configured Media Folders (%q vs %q) -- using Version %d's value",
				chosen.version, v.version, chosen.media, v.media, chosen.version)
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
