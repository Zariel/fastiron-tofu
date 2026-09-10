.PHONY: build test test-console vet check fmt

build:
	go build -o bin/terraform-provider-fastiron ./cmd/terraform-provider-fastiron

test:
	go test -race ./...

vet:
	go vet ./...

test-console:
	python3 -m unittest discover -s tools -p '*_test.py'

fmt:
	gofumpt -w .

check: test test-console vet
