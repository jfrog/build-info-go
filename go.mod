module github.com/jfrog/build-info-go

go 1.15

require (
	github.com/golang/snappy v0.0.3 // indirect
	github.com/google/go-cmp v0.5.6 // indirect
	github.com/jfrog/gocmd v0.5.0
	github.com/jfrog/jfrog-client-go v1.5.0
	github.com/stretchr/testify v1.7.0
	golang.org/x/net v0.0.0-20210503060351-7fd8e65b6420 // indirect
	golang.org/x/sys v0.0.0-20210823070655-63515b42dcdf // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20210107192922-496545a6307b // indirect
)

//replace github.com/jfrog/gocmd => ../gocmd

//replace github.com/jfrog/jfrog-client-go => ../jfrog-client-go
