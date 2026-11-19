package utils

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/jfrog/gofrog/version"
)

// maxExtractorVersionTries is the number of newest versions tried before giving up.
const maxExtractorVersionTries = 5

// ErrBlockedByCuration is wrapped by a download function when the server refuses a file because of a JFrog Curation policy.
var ErrBlockedByCuration = errors.New("blocked by JFrog Curation")

// ErrInvalidExtractorVersion is wrapped by an ExtractorVersionsFunc when the user asked for a version that is not valid.
// The lookup is not retried with another source, the command fails.
var ErrInvalidExtractorVersion = errors.New("invalid extractor version")

// ErrLookupNotRepeated tells that the lookup of the versions was not tried again, because an earlier lookup failed a short time ago.
// The user was told about that failure then, so it is not a new warning.
var ErrLookupNotRepeated = errors.New("the lookup of the extractor versions is not repeated yet")

var stableVersionRegex = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// IsStableVersion returns true for a version of the form x.y.z. Such a version is also safe to use as a folder name.
func IsStableVersion(v string) bool {
	return stableVersionRegex.MatchString(v)
}

// ExtractorVersions is the answer of an ExtractorVersionsFunc.
type ExtractorVersions struct {
	// The versions of the major line, newest first.
	Versions []string
	// True if the user chose the version. It is used as is: no other version is tried.
	Pinned bool
	// The name of the environment variable that pins the version. It is shown in error messages.
	PinEnv string
	// Verify is optional. It asks the server if a version is still allowed, for a version whose jar is already on disk,
	// because the server is not asked when the jar exists. It returns an error that wraps ErrBlockedByCuration for a blocked version.
	// Any other error means that the server could not tell. The jar on disk is used then.
	Verify func(version string) error
}

// ExtractorVersionsFunc lists the available versions of an extractor that belong to the given major line.
type ExtractorVersionsFunc func(majorLine string) (ExtractorVersions, error)

type mavenMetadata struct {
	Versions []string `xml:"versioning>versions>version"`
}

// ParseExtractorVersions returns the stable (x.y.z) versions of a maven-metadata.xml file, newest first, without duplicates.
func ParseExtractorVersions(metadata []byte) ([]string, error) {
	var parsed mavenMetadata
	if err := xml.Unmarshal(metadata, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse maven-metadata.xml: %w", err)
	}
	var versions []string
	for _, v := range parsed.Versions {
		if v = strings.TrimSpace(v); IsStableVersion(v) {
			versions = append(versions, v)
		}
	}
	sortNewestFirst(versions)
	return slices.Compact(versions), nil
}

// FilterMajorLine keeps the versions whose major number equals majorLine.
func FilterMajorLine(versions []string, majorLine string) []string {
	var filtered []string
	for _, v := range versions {
		if MajorLine(v) == majorLine {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// MajorLine returns the major number of a version, e.g. "5" for "5.2.5".
func MajorLine(v string) string {
	major, _, _ := strings.Cut(v, ".")
	return major
}

func sortNewestFirst(versions []string) {
	// Compare returns -1 when its argument is older than the receiver.
	sort.Slice(versions, func(i, j int) bool {
		return version.NewVersion(versions[i]).Compare(versions[j]) < 0
	})
}

// DownloadNewestExtractor downloads the newest allowed version of an extractor, and returns the version it chose.
//
// versionsFunc lists the versions of the same major line as defaultVersion, newest first. If it is nil, defaultVersion is used.
// The newest maxExtractorVersionTries are tried in order, and a version blocked by Curation is skipped.
// A version that the user chose (pinned) is used as is, with no other version tried.
//
// The versions that the lookup lists are not bounded below by defaultVersion: the newest version the repository lists is used,
// even if it is older than defaultVersion, and the user is told. An offline mirror may hold only that version.
//
// If the lookup fails or finds nothing, defaultVersion is used. If no version can be downloaded, the newest jar of that major line
// that is already in baseDir is used, so a cached jar keeps working. If there is none, defaultVersion is the last resort.
// download must skip the download of a jar that exists. jarFileFormat is the file name of a jar, with the version as its argument.
func DownloadNewestExtractor(versionsFunc ExtractorVersionsFunc, baseDir, defaultVersion, jarFileFormat string,
	download func(version string) error, logger Log) (string, error) {
	if versionsFunc == nil {
		return defaultVersion, download(defaultVersion)
	}
	found, err := versionsFunc(MajorLine(defaultVersion))
	if errors.Is(err, ErrInvalidExtractorVersion) {
		return "", err
	}
	if err != nil {
		logLookupFailure(err, found.PinEnv, logger)
	}
	walk := &extractorWalk{defaultVersion: defaultVersion, baseDir: baseDir, jarFileFormat: jarFileFormat,
		verify: found.Verify, download: download, logger: logger, blocked: map[string]bool{}}
	candidates := found.Versions
	switch {
	case found.Pinned:
		// Used as is: no cached jar or other version replaces it.
		return downloadNewestAllowed(candidates, found.PinEnv, true, download, logger)
	case err != nil || len(candidates) == 0:
		candidates = []string{defaultVersion}
	default:
		candidates = candidates[:min(len(candidates), maxExtractorVersionTries)]
	}
	chosen, err := chooseVersion(candidates, found.PinEnv, defaultVersion, walk.fetch, logger)
	if err == nil {
		return chosen, nil
	}
	if local := walk.newestCached(); local != "" {
		logger.Warn(fmt.Sprintf("Could not download a new extractor version. Using version %s from the local cache.%s\n%s", local, pinHint(found.PinEnv), err.Error()))
		return local, nil
	}
	// The version that worked before the lookup existed is the last resort on a machine with no jar.
	if !walk.defaultTried && download(defaultVersion) == nil {
		logger.Warn(fmt.Sprintf("Could not download a new extractor version. Using the default version %s.\n%s", defaultVersion, err.Error()))
		return defaultVersion, nil
	}
	return "", err
}

// extractorWalk is what the walk over the versions remembers.
type extractorWalk struct {
	defaultVersion, baseDir, jarFileFormat string
	verify                                 func(version string) error
	download                               func(version string) error
	logger                                 Log
	// True if the default version was tried: the walk stops at the first error that is not a Curation block.
	defaultTried bool
	// The versions that Curation blocked in this walk. A blocked version is not used as a cached jar either.
	blocked map[string]bool
}

// fetch makes one version available: it checks a jar on disk with the server, or downloads the jar.
func (w *extractorWalk) fetch(v string) error {
	w.defaultTried = w.defaultTried || v == w.defaultVersion
	err := verifyCachedJar(w.verify, w.baseDir, v, w.jarFileFormat, w.logger)
	if err == nil {
		err = w.download(v)
	}
	if errors.Is(err, ErrBlockedByCuration) {
		w.blocked[v] = true
	}
	return err
}

// newestCached returns the newest jar on disk that is still allowed, or "" if there is none.
// Each jar goes through fetch: the server is asked about it, and a version that Curation blocked is skipped.
func (w *extractorWalk) newestCached() string {
	for _, v := range cachedVersions(w.baseDir, w.defaultVersion, w.jarFileFormat, w.blocked) {
		if w.fetch(v) == nil {
			return v
		}
	}
	return ""
}

// chooseVersion downloads the first allowed version of the list, and tells the user if it is not the default one.
func chooseVersion(versions []string, pinEnv, defaultVersion string, download func(version string) error, logger Log) (string, error) {
	chosen, err := downloadNewestAllowed(versions, pinEnv, false, download, logger)
	if err != nil || chosen == defaultVersion {
		return chosen, err
	}
	message := "Using build-info extractor version " + chosen
	// Compare returns -1 when its argument is older than the receiver.
	if version.NewVersion(defaultVersion).Compare(chosen) < 0 {
		// A repository that lists nothing newer is used as it is: an offline mirror may hold only an older version.
		message += fmt.Sprintf(". It is older than %s, the default version of this CLI. The repository offers no newer allowed version.", defaultVersion)
	}
	logger.Info(message)
	return chosen, err
}

// logLookupFailure warns about a lookup that failed. A lookup that was not tried again is only a Debug message:
// an offline user would see the same warning in each command for as long as the lookup is not repeated.
func logLookupFailure(err error, pinEnv string, logger Log) {
	message := fmt.Sprintf("Failed to look up the latest extractor version: %s.%s", err.Error(), pinHint(pinEnv))
	if errors.Is(err, ErrLookupNotRepeated) {
		logger.Debug(message)
		return
	}
	logger.Warn(message)
}

// pinHint tells how to skip the lookup. It is empty if the name of the variable is not known.
func pinHint(pinEnv string) string {
	if pinEnv == "" {
		return ""
	}
	return fmt.Sprintf(" Set %s to a version to skip the lookup.", pinEnv)
}

// verifyCachedJar asks the server if a version is still allowed, if its jar is already on disk. A jar on disk is not downloaded again,
// so the server would not tell that Curation blocked the version after the first download.
// It returns an error only for a Curation block. If the server cannot tell, the jar on disk is used.
func verifyCachedJar(verify func(version string) error, baseDir, version, jarFileFormat string, logger Log) error {
	if verify == nil || !localJarExists(baseDir, version, jarFileFormat) {
		return nil
	}
	err := verify(version)
	if err != nil && !errors.Is(err, ErrBlockedByCuration) {
		logger.Debug(fmt.Sprintf("Could not check if extractor version %s is still allowed. Using the jar on disk: %s", version, err.Error()))
		return nil
	}
	return err
}

// localJarExists checks the jar, not the folder: a failed download leaves an empty folder.
func localJarExists(baseDir, version, jarFileFormat string) bool {
	exists, err := IsFileExists(filepath.Join(baseDir, version, fmt.Sprintf(jarFileFormat, version)), true)
	return err == nil && exists
}

// cachedVersions returns the versions in baseDir whose jar exists and whose major line is the one of defaultVersion, newest first.
// A version in 'exclude' is not returned.
func cachedVersions(baseDir, defaultVersion, jarFileFormat string, exclude map[string]bool) []string {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil
	}
	var versions []string
	for _, entry := range entries {
		v := entry.Name()
		if entry.IsDir() && IsStableVersion(v) && MajorLine(v) == MajorLine(defaultVersion) && !exclude[v] && localJarExists(baseDir, v, jarFileFormat) {
			versions = append(versions, v)
		}
	}
	sortNewestFirst(versions)
	return versions
}

// downloadNewestAllowed calls download for each version, in order, and returns the first version that succeeds.
// A version blocked by Curation is skipped. Any other error stops the walk.
// pinEnv is the name of the environment variable that pins a version. It is shown if all the versions are blocked.
func downloadNewestAllowed(versions []string, pinEnv string, pinned bool, download func(version string) error, logger Log) (string, error) {
	if len(versions) == 0 {
		return "", errors.New("no extractor version to download")
	}
	var blocked []string
	var blockedErr error
	for _, v := range versions {
		err := download(v)
		if err == nil {
			if len(blocked) > 0 {
				logger.Warn(fmt.Sprintf("Using extractor version %s, because newer versions are blocked by Curation: %s", v, strings.Join(blocked, ", ")))
			}
			return v, nil
		}
		if !errors.Is(err, ErrBlockedByCuration) {
			return "", err
		}
		// The error says why: a block that is remembered from an earlier run names the file to delete to try again.
		logger.Warn(fmt.Sprintf("Extractor version %s is blocked by Curation. Trying an older version.\n%s", v, err.Error()))
		blocked = append(blocked, v)
		blockedErr = err
	}
	return "", fmt.Errorf("all the extractor versions tried (%s) are blocked by Curation. %s.\n%w",
		strings.Join(blocked, ", "), blockedAdvice(pinEnv, pinned), blockedErr)
}

// blockedAdvice tells what the user can do when every version that was tried is blocked.
// A user who pinned the version has already set the variable, so the advice is to change it.
func blockedAdvice(pinEnv string, pinned bool) string {
	switch {
	case pinned && pinEnv != "":
		return fmt.Sprintf("Change or unset %s, or ask your Curation admin to allow the version", pinEnv)
	case pinEnv != "":
		return fmt.Sprintf("Ask your Curation admin to allow one of them, or pin an allowed version with the %s environment variable", pinEnv)
	}
	return "Ask your Curation admin to allow one of them"
}

// WriteFileAtomic writes the content of r to path. A reader of path sees the old file or the whole new one, never a part of it.
// The content goes to a temporary file in the same folder first, and then the file is renamed.
func WriteFileAtomic(path string, r io.Reader, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0777); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.part")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			// Cleanup after a failure that is already returned.
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = io.Copy(tmp, r); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
