.PHONY: build version bump tag dist release test test-short test-e2e test-live test-startup vet

PART ?= patch
export PART

build:
	sh scripts/release.sh build

version:
	@sh scripts/release.sh version

bump:
	@sh scripts/release.sh bump

tag:
	sh scripts/release.sh tag

dist:
	sh scripts/release.sh dist

release:
	sh scripts/release.sh release

test:
	go test -count=1 -race ./...

test-short:
	go test -short ./...

test-e2e:
	go test -count=1 -v ./tests/e2e -run '^(TestPTY|TestCLI|TestCLIVersion)$$'

test-live:
	go test -count=1 -v ./tests/e2e -run '^TestLive$$' -live

test-startup:
	go test -count=1 -v ./tests/e2e -run '^TestStartup$$' -startup

vet:
	go vet ./...
