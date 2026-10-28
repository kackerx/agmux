# make check: what CI runs (.github/workflows/ci.yml), before pushing.
DEADCODE = golang.org/x/tools/cmd/deadcode@v0.52.0

.PHONY: check
check:
	go vet ./...
	go test -race ./...
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go mod tidy
	git diff --exit-code go.mod go.sum
	@out="$$(go run $(DEADCODE) ./...)" || exit 1; test -z "$$out" || { echo "$$out"; echo "dead code: only tests call these (delete them, or move them into a _test.go)"; exit 1; }
