.PHONY: build test vet check fmt

build:
	go build -o bin/terraform-provider-fastiron ./cmd/terraform-provider-fastiron

test:
	go test -race ./...

vet:
	go vet ./...

check: test vet

fmt:
	gofumpt -w .
