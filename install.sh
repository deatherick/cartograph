#!/bin/sh
# Cartograph installer — docs/requirements/phase9-global-install-and-daemon.md
# / ADR-0026 (Phase 9). Downloads a prebuilt ctx/ctxd/ctxmcp release for this
# machine's OS/arch and places them on PATH — no sudo, no cloning this repo,
# no Go toolchain required, matching that requirements document's own
# explicit ask.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/deatherick/cartograph/main/install.sh | sh
#
# Env vars (all optional):
#   CARTOGRAPH_INSTALL_DIR   where to place the binaries (default: ~/.local/bin)
#   CARTOGRAPH_VERSION       a specific release tag, e.g. v0.1.0 (default: latest)
#
# Only macOS and Linux are supported — this project's own CI matrix
# (.github/workflows/ci.yml, release.yml) is macOS + Linux only, and
# Windows is explicitly out of scope per
# docs/requirements/phase9-global-install-and-daemon.md's own
# "Explicitly not decided here" section.
set -e

REPO="deatherick/cartograph"
INSTALL_DIR="${CARTOGRAPH_INSTALL_DIR:-$HOME/.local/bin}"

os() {
	case "$(uname -s)" in
	Darwin) echo "darwin" ;;
	Linux) echo "linux" ;;
	*)
		echo "cartograph: unsupported OS $(uname -s) — only macOS and Linux are supported (see docs/requirements/phase9-global-install-and-daemon.md)" >&2
		exit 1
		;;
	esac
}

arch() {
	case "$(uname -m)" in
	arm64 | aarch64) echo "arm64" ;;
	x86_64 | amd64) echo "amd64" ;;
	*)
		echo "cartograph: unsupported architecture $(uname -m)" >&2
		exit 1
		;;
	esac
}

OS="$(os)"
ARCH="$(arch)"
ASSET="cartograph_${OS}_${ARCH}.tar.gz"

if [ -n "$CARTOGRAPH_VERSION" ]; then
	URL="https://github.com/${REPO}/releases/download/${CARTOGRAPH_VERSION}/${ASSET}"
else
	URL="https://github.com/${REPO}/releases/latest/download/${ASSET}"
fi

echo "cartograph: downloading ${ASSET}..."
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

if ! curl -fsSL "$URL" -o "$TMPDIR/$ASSET"; then
	cat >&2 <<EOF
cartograph: could not download a prebuilt release for ${OS}/${ARCH} from
  ${URL}
This usually means no release has been published yet for this platform.
If you have Go 1.27+ and Node.js installed, build from source instead:
  git clone https://github.com/${REPO}.git
  cd cartograph && make build
  cp bin/ctx bin/ctxd bin/ctxmcp ${INSTALL_DIR}/
EOF
	exit 1
fi

tar -xzf "$TMPDIR/$ASSET" -C "$TMPDIR"

mkdir -p "$INSTALL_DIR"
for bin in ctx ctxd ctxmcp; do
	if [ -f "$TMPDIR/$bin" ]; then
		cp "$TMPDIR/$bin" "$INSTALL_DIR/$bin"
		chmod +x "$INSTALL_DIR/$bin"
	fi
done

echo "cartograph: installed ctx, ctxd, ctxmcp to ${INSTALL_DIR}"

case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	echo ""
	echo "NOTE: ${INSTALL_DIR} is not on your PATH. Add this to your shell profile:"
	echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
	;;
esac

echo ""
echo "Next steps:"
echo "  ctx index ~/path/to/some/project"
echo "  ctx service install     # run ctxd as a persistent background service (launchd/systemd --user)"
