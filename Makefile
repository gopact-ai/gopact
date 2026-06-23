.PHONY: test fmt vet

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...
