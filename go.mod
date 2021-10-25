module github.com/jfrog/build-info-go

go 1.15

require (
	github.com/jfrog/gocmd v0.5.0
	github.com/jfrog/jfrog-client-go v1.5.1
	github.com/stretchr/testify v1.7.0
)

replace github.com/jfrog/gocmd => github.com/asafgabai/gocmd v0.1.20-0.20211025124110-b76b3a6186df

replace github.com/jfrog/jfrog-client-go => github.com/asafgabai/jfrog-client-go v0.18.1-0.20211025084412-39087fc85727
