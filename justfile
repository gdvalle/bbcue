export CGO_ENABLED := "0"

# Regenerate README, workflows, and run all tests.
all: fmt readme test generate

# Run all tests and checks, and ensure no files were modified during the process.
ci: all
    git diff --exit-code

# Generate CUE output fields for the github actions workflow
generate:
    go run ./cmd/bbcue .github

# Build the bbcue binary with VCS info stamped in.
build:
    go build -o bbcue ./cmd/bbcue

# Run all tests.
test:
    go test ./...

fmt:
    go fmt ./...

# Run all tests verbosely.
test-v:
    go test -v -count=1 ./...

# Regenerate README.md from the template and txtar test fixtures.
readme:
    go run ./internal/genreadme

# Install the binary.
install:
    go install ./cmd/bbcue

# Clean build artifacts.
clean:
    rm -f bbcue
