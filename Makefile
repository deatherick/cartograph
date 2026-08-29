.PHONY: build test lint bench clean

build:
	go build -o bin/ctx ./cmd/ctx
	go build -o bin/ctxd ./cmd/ctxd
	go build -o bin/ctxbench ./cmd/ctxbench

test:
	go test ./... -race

lint:
	golangci-lint run ./...

bench: build
	./bin/ctxbench --baseline

clean:
	rm -rf bin/
