.PHONY: build run test check race i18n-check package
build:
	./scripts/project-go build -o bin/lazyclash .
run: build
	./bin/lazyclash
test:
	./scripts/project-go test ./... github.com/charmbracelet/ultraviolet/...
check:
	./scripts/project-go test ./... github.com/charmbracelet/ultraviolet/...
	./scripts/project-go vet ./... github.com/charmbracelet/ultraviolet/...
race:
	./scripts/project-go test -race ./... github.com/charmbracelet/ultraviolet/...
i18n-check:
	./scripts/project-go test ./internal/i18n

package:
	./scripts/package.sh "$(or $(VERSION),dev)"
