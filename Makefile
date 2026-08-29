.PHONY: build web test lint bench bench-real clean

# web builds the React frontend (web/) and copies its output into
# internal/httpserver/web/ — the directory internal/httpserver.go embeds
# via go:embed. go:embed directives cannot reference a parent directory
# (no "../"), so the built assets have to physically live inside the Go
# package tree; this copy step is that bridge. See
# docs/adr/0015-react-web-ui.md for why the web UI needs a Node/npm build
# step at all (a reversal of ADR-0013's original no-Node choice).
web:
	cd web && npm install && npm run build
	rm -rf internal/httpserver/web
	mkdir -p internal/httpserver/web
	cp -r web/dist/. internal/httpserver/web/

build: web
	go build -o bin/ctx ./cmd/ctx
	go build -o bin/ctxd ./cmd/ctxd
	go build -o bin/ctxbench ./cmd/ctxbench
	go build -o bin/ctxmcp ./cmd/ctxmcp

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
