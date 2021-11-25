module github.com/jfrog/build-info-go

go 1.15

require (
	github.com/jfrog/gocmd v0.5.5
	github.com/jfrog/gofrog v1.1.0
	github.com/jfrog/jfrog-client-go v1.6.1
	github.com/stretchr/testify v1.7.0
	github.com/urfave/cli/v2 v2.3.0
	golang.org/x/text v0.3.7 // indirect
)

exclude (
	golang.org/x/text v0.3.3
	golang.org/x/text v0.3.5
)

// replace github.com/jfrog/gocmd => github.com/jfrog/gocmd v0.5.5-0.20211124162113-60531e4d9053

replace github.com/jfrog/jfrog-client-go => github.com/jfrog/jfrog-client-go v1.6.2-0.20211124162552-294b033f185b

//replace github.com/jfrog/gofrog => github.com/jfrog/gofrog v1.0.7-0.20211107071406-54da7fb08599
