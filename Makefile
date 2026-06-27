.PHONY: bin test lint cover cover-html check hooks

bin:
	goreleaser build --snapshot --clean --single-target

test:
	go test -race ./...

lint:
	golangci-lint run

cover:
	go test -coverprofile=coverage.txt -covermode=atomic ./...
	go tool cover -func=coverage.txt | tail -1

cover-html: cover
	go tool cover -html=coverage.txt

check: lint cover
	go-test-coverage --config=.testcoverage.yml

hooks:
	lefthook install
