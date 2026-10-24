default: fmt lint test build

# Build the provider binary.
build:
	go build -v ./...

# Install the provider into $GOPATH/bin, for a dev_overrides block.
install: build
	go install -v ./...

# Lint with golangci-lint v2 (.golangci.yml).
lint:
	golangci-lint run

# Format Go and the example Terraform.
fmt:
	gofmt -s -w -e .
	terraform fmt -recursive ./examples/

# Regenerate docs/ from the schema, templates/ and examples/.
generate:
	cd tools; go generate ./...

# Unit tests and the Terraform-driven tests against the fake API. They need
# terraform on PATH and nothing else: no account, no network.
test:
	go test -v -cover -timeout=300s -parallel=4 ./...

# Live tests against a real Plotwork API. Needs PLOTWORK_API_KEY on a Pro
# account; set PLOTWORK_API_URL to aim at a preview environment.
testacc:
	TF_ACC=1 go test -v -cover -timeout 30m -run 'TestAcc' ./...

.PHONY: default build install lint fmt generate test testacc
