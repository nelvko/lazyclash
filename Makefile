.PHONY: build run test check race i18n-check package
record = python3 scripts/test_artifacts.py
packages = ./... github.com/charmbracelet/ultraviolet/...

build:
	./scripts/project-go build -o bin/lazyclash .
run: build
	./bin/lazyclash
test:
	$(record) test -- ./scripts/project-go test $(packages)
check: test
	$(record) vet -- ./scripts/project-go vet $(packages)
race:
	$(record) race -- ./scripts/project-go test -race $(packages)
i18n-check:
	$(record) i18n-check -- ./scripts/project-go test ./internal/i18n

package:
	./scripts/package.sh "$(or $(VERSION),dev)"
