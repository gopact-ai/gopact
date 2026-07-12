.PHONY: check phase1 test race tidy fmt vet security coverage

check: tidy test vet

phase1: test race vet security

test:
	go test -count=1 ./...

race:
	go test -race -count=1 ./...

tidy:
	go mod tidy -diff

fmt:
	gofmt -w .

vet:
	go vet ./...

security:
	govulncheck ./...

coverage:
	go test -coverprofile=coverage.out ./...
