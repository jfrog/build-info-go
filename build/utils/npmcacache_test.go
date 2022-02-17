package utils

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIntegrityToSha(t *testing.T) {
	hashAlgorithm, hash, err := integrityToSha("sha512-dWe4nWO/ruEOY7HkUJ5gFt1DCFV9zPRoJr8pV0/ASQermOZjtq8jMjOprC0Kd10GLN+l7xaUPvxzJFWtxGu8Fg==")
	assert.NoError(t, err)
	assert.Equal(t, "7567b89d63bfaee10e63b1e4509e6016dd4308557dccf46826bf29574fc04907ab98e663b6af233233a9ac2d0a775d062cdfa5ef16943efc732455adc46bbc16", hash)
	assert.Equal(t, "sha512", hashAlgorithm)

	hashAlgorithm, hash, err = integrityToSha("sha1-Z29us8OZl8LuGsOpJP1hJHSPV40=")
	assert.NoError(t, err)
	assert.Equal(t, "676f6eb3c39997c2ee1ac3a924fd6124748f578d", hash)
	assert.Equal(t, "sha1", hashAlgorithm)
}

func TestGetDepTarball(t *testing.T) {
	cacache := NewNpmCacache(filepath.Join("..", "testdata", "npm", "_cacache"))
	path, err := cacache.GetTarball("sha512-dWe4nWO/ruEOY7HkUJ5gFt1DCFV9zPRoJr8pV0/ASQermOZjtq8jMjOprC0Kd10GLN+l7xaUPvxzJFWtxGu8Fg==")
	assert.NoError(t, err)
	assert.True(t, strings.HasSuffix(path, filepath.Join("testdata", "npm", "_cacache", "content-v2", "sha512", "75", "67", "b89d63bfaee10e63b1e4509e6016dd4308557dccf46826bf29574fc04907ab98e663b6af233233a9ac2d0a775d062cdfa5ef16943efc732455adc46bbc16")))

	path, err = cacache.GetTarball("sha1-Z29us8OZl8LuGsOpJP1hJHSPV40=")
	assert.NoError(t, err)
	assert.True(t, strings.HasSuffix(path, filepath.Join("testdata", "npm", "_cacache", "content-v2", "sha1", "67", "6f", "6eb3c39997c2ee1ac3a924fd6124748f578d")))
}

func TestGetDepInfo(t *testing.T) {
	cacache := NewNpmCacache(filepath.Join("..", "testdata", "npm", "_cacache"))
	info, err := cacache.GetInfo("ansi-regex@5.0.0")
	assert.NoError(t, err)
	assert.Equal(t, "sha512-bY6fj56OUQ0hU1KjFNDQuJFezqKdrAyFdIevADiqrWHwSlbmBNMHp5ak2f40Pm8JTFyM2mqxkG6ngkHO11f/lg==", info.Integrity)
}
