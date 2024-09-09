package utils

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jfrog/build-info-go/utils"
	"github.com/jfrog/gofrog/crypto"
	"path/filepath"
	"strings"
)

// npm stores cache data in an opaque directory within the configured cache, named _cacache.
// This directory is a cacache-based content-addressable cache that stores all http request data as well as other package-related data.
//
// Cacache is a Node.js library for managing local key and content address caches.
// It was written to be used as npm's local cache, but can just as easily be used on its own.
//
// Here, we're using our implementation for Cacache as there is no method exposed through npm to inspect or directly manage the contents of this cache.
type cacache struct {
	cachePath string
}

func NewNpmCacache(cachePath string) *cacache {
	return &cacache{cachePath: cachePath}
}

// Return the tarball path base on the supplied integrity.
// integrity - A sha512 or sha1 of the dependency tarball that was unpacked in node_modules (https://w3c.github.io/webappsec-subresource-integrity/).
func (c *cacache) GetTarball(integrity string) (string, error) {
	hashAlgorithms, hash, err := integrityToSha(integrity)
	if err != nil {
		return "", err
	}
	if len(hash) < 5 {
		return "", errors.New("failed to calculate npm dependencies tree. Bad dependency integrity " + integrity)
	}
	// Current format of content file path:
	//
	// sha512-BaSE64Hex= ->
	// ~/.my-cache/content-v2/sha512/ba/da/55deadbeefc0ffee
	tarballPath := filepath.Join(c.cachePath, "content-v2", hashAlgorithms, hash[0:2], hash[2:4], hash[4:])
	found, err := utils.IsFileExists(tarballPath, false)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("failed to locate dependency integrity '" + integrity + "' tarball at " + tarballPath)
	}
	return tarballPath, nil
}

// Integrity string has the pattern of: hashAlgorithms-digest.
// Return the hashAlgorithm and transform the diegest to the actual sha.
func integrityToSha(integrity string) (hashAlgorithm string, sha string, err error) {
	data := strings.SplitN(integrity, "-", 2)
	if len(data) != 2 {
		return "", "", errors.New("the integrity '" + integrity + "' has bad format (valid format is HashAlgorithms-Hash)")
	}
	var integrityDigest string
	hashAlgorithm, integrityDigest = data[0], data[1]
	decoded, err := base64.StdEncoding.DecodeString(integrityDigest)
	if err != nil {
		err = errors.New("failed to decode integrity hash. Error: " + err.Error())
		return
	}
	sha = hex.EncodeToString(decoded)
	return
}

type cacacheInfo struct {
	Integrity string
}

// Looks up key in the cache index (~/.my-cache/index-v5/...), and return information about the entry.
// id - Dependency specifier like foo@version.
func (c *cacache) GetInfo(id string) (*cacacheInfo, error) {
	// Extraction by key.
	idKey := "pacote:tarball:" + id
	path, err := c.getIndexByKey(idKey)
	if err != nil {
		return nil, err
	}
	// The first line is always empty.
	lines, err := utils.ReadNLines(path, 2)
	if err != nil {
		return nil, err
	}
	if len(lines) != 2 {
		return nil, errors.New("the index entry " + path + " in npm cache is empty")
	}
	// To parse the json content, split it from the rest.
	indexData := strings.Split(lines[1], "\t")
	// Format of each line line: hash \t entry-content (dependency info).
	if len(indexData) != 2 {
		return nil, errors.New("the index entry " + path + " has an invalid format")
	}
	var result *cacacheInfo
	return result, json.Unmarshal([]byte(indexData[1]), &result)
}

// Current format of index file path:
//
// "pacote:tarball:ansi-regex@5.0.0" ->
// ~/.my-cache/index-v5/4e/22/eb8971d3255ba68fad66a1be245aaf480c23e8b02cf0dae7022549aece7c
func (c *cacache) getIndexByKey(key string) (string, error) {
	hashMap, err := crypto.CalcChecksums(strings.NewReader(key), crypto.SHA256)
	if err != nil {
		return "", err
	}
	hashedKey := hashMap[crypto.SHA256]
	return c.getIndexByHash(hashedKey)
}

func (c *cacache) getIndexByHash(hash string) (string, error) {
	path := filepath.Join(c.cachePath, "index-v5", hash[0:2], hash[2:4], hash[4:])
	found, err := utils.IsFileExists(path, false)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New(hash + " is not found in " + path)
	}
	return path, nil
}
