module github.com/jfrog/build-info-go

go 1.15

require (
	github.com/jfrog/gofrog v1.1.0
	github.com/jfrog/jfrog-client-go v1.6.1
	github.com/stretchr/testify v1.7.0
	github.com/urfave/cli/v2 v2.3.0
)

//replace github.com/jfrog/jfrog-client-go => ../jfrog-client-go

replace github.com/jfrog/gofrog => github.com/asafgabai/gofrog v1.0.7-0.20211124142550-392f9a68eb53
