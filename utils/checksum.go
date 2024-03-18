package utils

import (
	"bufio"
	"errors"

	//#nosec G501 -- md5 is supported by Artifactory.
	"crypto/md5"
	//#nosec G505 -- sha1 is supported by Artifactory.
	"crypto/sha1"
	"fmt"
	"hash"
	"io"
	"os"

	"github.com/minio/sha256-simd"
)

type Algorithm int

const (
	MD5 Algorithm = iota
	SHA1
	SHA256
)

var algorithmFunc = map[Algorithm]func() hash.Hash{
	// Go native crypto algorithms:
	MD5:  md5.New,
	SHA1: sha1.New,
	// sha256-simd algorithm:
	SHA256: sha256.New,
}

func GetFileChecksums(filePath string, checksumType ...Algorithm) (checksums map[Algorithm]string, err error) {
	file, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer func() {
		err = errors.Join(err, file.Close())
	}()
	return CalcChecksums(file, checksumType...)
}

// CalcChecksums calculates all hashes at once using AsyncMultiWriter. The file is therefore read only once.
func CalcChecksums(reader io.Reader, checksumType ...Algorithm) (map[Algorithm]string, error) {
	hashes, err := calcChecksums(reader, checksumType...)
	if err != nil {
		return nil, err
	}
	results := sumResults(hashes)
	return results, nil
}

// CalcChecksumsBytes calculates hashes like `CalcChecksums`, returns result as bytes
func CalcChecksumsBytes(reader io.Reader, checksumType ...Algorithm) (map[Algorithm][]byte, error) {
	hashes, err := calcChecksums(reader, checksumType...)
	if err != nil {
		return nil, err
	}
	results := sumResultsBytes(hashes)
	return results, nil
}

func calcChecksums(reader io.Reader, checksumType ...Algorithm) (map[Algorithm]hash.Hash, error) {
	hashes := getChecksumByAlgorithm(checksumType...)
	var multiWriter io.Writer
	pageSize := os.Getpagesize()
	sizedReader := bufio.NewReaderSize(reader, pageSize)
	var hashWriter []io.Writer
	for _, v := range hashes {
		hashWriter = append(hashWriter, v)
	}
	multiWriter = AsyncMultiWriter(hashWriter...)
	_, err := io.Copy(multiWriter, sizedReader)
	if err != nil {
		return nil, err
	}
	return hashes, nil
}

func sumResults(hashes map[Algorithm]hash.Hash) map[Algorithm]string {
	results := map[Algorithm]string{}
	for k, v := range hashes {
		results[k] = fmt.Sprintf("%x", v.Sum(nil))
	}
	return results
}

func sumResultsBytes(hashes map[Algorithm]hash.Hash) map[Algorithm][]byte {
	results := map[Algorithm][]byte{}
	for k, v := range hashes {
		results[k] = v.Sum(nil)
	}
	return results
}

func getChecksumByAlgorithm(checksumType ...Algorithm) map[Algorithm]hash.Hash {
	hashes := map[Algorithm]hash.Hash{}
	if len(checksumType) == 0 {
		for k, v := range algorithmFunc {
			hashes[k] = v()
		}
		return hashes
	}

	for _, v := range checksumType {
		hashes[v] = algorithmFunc[v]()
	}
	return hashes
}
