.PHONY: build test test-short test-e2e test-live test-startup vet

build:
	go build -o bin/tosh ./cmd/tosh

test:
	go test -count=1 -race ./...

test-short:
	go test -short ./...

test-e2e:
	go test -count=1 -v ./tests/e2e -run '^(TestPTY|TestCLI)$$'

test-live:
	go test -count=1 -v ./tests/e2e -run '^TestLive$$' -live

test-startup:
	go test -count=1 -v ./tests/e2e -run '^TestStartup$$' -startup

vet:
	go vet ./...
