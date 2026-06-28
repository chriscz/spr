.PHONY: bin test lint cover cover-html check hooks

bin:
	goreleaser build --snapshot --clean --single-target

test:
	go test -race ./...

lint:
	golangci-lint run

cover:
	rm -rf .coverdata && mkdir -p .coverdata/unit .coverdata/e2e .coverdata/merged
	SPR_E2E_COVERDIR=$(CURDIR)/.coverdata/e2e \
		go test -covermode=atomic ./... -args -test.gocoverdir=$(CURDIR)/.coverdata/unit
	# The e2e harness writes covdata into unique run-* subdirs of .coverdata/e2e
	# (so parallel/repeated runs never clobber each other); covdata reads files
	# directly in each -i dir and is not recursive, so enumerate the leaf dirs.
	go tool covdata merge \
		-i=$$(echo $(CURDIR)/.coverdata/unit $$(find $(CURDIR)/.coverdata/e2e -name 'covmeta.*' -exec dirname {} \; | sort -u) | tr ' ' ',') \
		-o=$(CURDIR)/.coverdata/merged
	go tool covdata textfmt -i=$(CURDIR)/.coverdata/merged -o=coverage.txt
	go tool cover -func=coverage.txt | tail -1

cover-html: cover
	go tool cover -html=coverage.txt

check: lint cover
	go-test-coverage --config=.testcoverage.yml

hooks:
	lefthook install
