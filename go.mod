module github.com/jfrog/build-info-go

go 1.15

require (
	github.com/CycloneDX/cyclonedx-go v0.4.0
	github.com/buger/jsonparser v1.1.1
	github.com/jfrog/gofrog v1.1.1
	github.com/kr/text v0.2.0 // indirect
	github.com/minio/sha256-simd v1.0.1-0.20210617151322-99e45fae3395
	github.com/pkg/errors v0.8.0
	github.com/stretchr/testify v1.7.0
	github.com/urfave/cli/v2 v2.3.0
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

exclude (
	golang.org/x/text v0.3.3
	golang.org/x/text v0.3.5
)

// replace github.com/jfrog/gofrog => github.com/jfrog/gofrog v1.0.7-0.20211128152632-e218c460d703
