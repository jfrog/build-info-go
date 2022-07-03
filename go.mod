module github.com/jfrog/build-info-go

go 1.17

require (
	github.com/CycloneDX/cyclonedx-go v0.5.1
	github.com/buger/jsonparser v1.1.1
	github.com/jfrog/gofrog v1.2.0
	github.com/minio/sha256-simd v1.0.1-0.20210617151322-99e45fae3395
	github.com/pkg/errors v0.9.1
	github.com/stretchr/testify v1.8.0
	github.com/urfave/cli/v2 v2.4.0
	github.com/xeipuuv/gojsonschema v1.2.0
)

require (
	github.com/cpuguy83/go-md2man/v2 v2.0.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/klauspost/cpuid/v2 v2.0.6 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/xeipuuv/gojsonpointer v0.0.0-20180127040702-4e3ac2762d5f // indirect
	github.com/xeipuuv/gojsonreference v0.0.0-20180127040603-bd5ef7bd5415 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

exclude (
	golang.org/x/text v0.3.3
	golang.org/x/text v0.3.5
)

// replace github.com/jfrog/gofrog => github.com/jfrog/gofrog v1.0.7-0.20211128152632-e218c460d703
