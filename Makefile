.PHONY: build test check race i18n-check package
build:
	go build -o bin/lazyclash .
test:
	go test ./...
check:
	go test ./...
	go vet ./...
race:
	go test -race ./...
i18n-check:
	go test ./internal/i18n

package:
	./scripts/package.sh "$(or $(VERSION),dev)"
