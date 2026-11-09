.PHONY: build test check race i18n-check package
build:
	go build -o bin/lazyclash .
test:
	go test ./... github.com/charmbracelet/ultraviolet/...
check:
	go test ./... github.com/charmbracelet/ultraviolet/...
	go vet ./... github.com/charmbracelet/ultraviolet/...
race:
	go test -race ./... github.com/charmbracelet/ultraviolet/...
i18n-check:
	go test ./internal/i18n

package:
	./scripts/package.sh "$(or $(VERSION),dev)"
