.PHONY: build test lint fmt vet check clean

build:
	go build -o nv .

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

vet:
	go vet ./...

check: fmt vet test

clean:
	rm -f nv
