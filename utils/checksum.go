package utils

import (
	"bufio"
	"crypto/md5"
	"crypto/sha1"
	"fmt"
	"github.com/jfrog/build-info-go/entities"
	"github.com/minio/sha256-simd"
	"hash"
	"io"
	"os"
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

func GetFileChecksums(filePath string) (checksums *entities.Checksum, err error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() {
		e := file.Close()
		if err == nil {
			err = e
		}
	}()
	checksumInfo, err := CalcChecksums(file)
	if err != nil {
		return nil, err
	}
	return &entities.Checksum{Md5: checksumInfo[MD5], Sha1: checksumInfo[SHA1], Sha256: checksumInfo[SHA256]}, nil
}

// CalcChecksums calculates all hashes at once using AsyncMultiWriter. The file is therefore read only once.
func CalcChecksums(reader io.Reader, checksumType ...Algorithm) (map[Algorithm]string, error) {
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
	results := sumResults(hashes)
	return results, nil
}

func sumResults(hashes map[Algorithm]hash.Hash) map[Algorithm]string {
	results := map[Algorithm]string{}
	for k, v := range hashes {
		results[k] = fmt.Sprintf("%x", v.Sum(nil))
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
