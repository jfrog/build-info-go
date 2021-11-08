set CGO_ENABLED=0
go build -o bi.exe -ldflags "-w -extldflags -static" main.go
