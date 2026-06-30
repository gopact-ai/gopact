.PHONY: check test race tidy fmt vet lint coverage examples graph a2a-mesh security

check:
	git diff --check
	go mod tidy
	git diff --exit-code
	go test -count=1 ./...
	go test -race -count=1 ./...
	go vet ./...
	golangci-lint run ./...
	go test -coverprofile=coverage.out ./...
	go test -run '^Example' ./...
	go test -count=1 ./graph ./gopacttest/graphconformance
	go test -count=1 ./a2a ./gopacttest/a2aconformance
	govulncheck ./...

test:
	go test -count=1 ./...

race:
	go test -race -count=1 ./...

tidy:
	go mod tidy
	git diff --exit-code

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	golangci-lint run ./...

coverage:
	go test -coverprofile=coverage.out ./...

examples:
	go test -run '^Example' ./...

graph:
	go test -count=1 ./graph ./gopacttest/graphconformance

a2a-mesh:
	go test -count=1 ./a2a ./gopacttest/a2aconformance

security:
	govulncheck ./...
