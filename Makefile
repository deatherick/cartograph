.PHONY: build test lint bench bench-real clean

build:
	go build -o bin/ctx ./cmd/ctx
	go build -o bin/ctxd ./cmd/ctxd
	go build -o bin/ctxbench ./cmd/ctxbench

test:
	go test ./... -race

lint:
	golangci-lint run ./...

bench: build
	./bin/ctxbench --baseline --capsule --budget 2500

# bench-real runs the same measurement against a real, external repo instead
# of the synthetic fixture — clones it to ~/code/_ref if not already present.
# Never vendored into this repo; see docs/benchmarks/README.md.
bench-real: build
	@[ -d ~/code/_ref/realworld-ts ] || git clone --depth 1 \
		https://github.com/skopekreep/typescript-node-express-realworld-example-app \
		~/code/_ref/realworld-ts
	./bin/ctxbench --baseline --capsule --budget 2500 --fixtures-root ~/code/_ref --tasks fixtures/tasks/realworld-ts.json

clean:
	rm -rf bin/
