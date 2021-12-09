module github.com/jfrog/build-info-go

go 1.15

require (
	github.com/jfrog/gofrog v1.1.0
	github.com/stretchr/testify v1.7.0
	github.com/urfave/cli/v2 v2.3.0
)

exclude (
	golang.org/x/text v0.3.3
	golang.org/x/text v0.3.5
)

replace github.com/jfrog/gofrog => github.com/jfrog/gofrog v1.0.7-0.20211128152632-e218c460d703
