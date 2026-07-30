#!/bin/sh
set -eu

REPO="YoanWai/agent-manager"
BINARY="agent-manager"

say() {
	printf '%s\n' "$*"
}

err() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"
}

detect_os() {
	case $(uname -s) in
	Darwin) echo darwin ;;
	Linux) echo linux ;;
	*) err "unsupported operating system: $(uname -s). On Windows, run this inside WSL2." ;;
	esac
}

detect_arch() {
	case $(uname -m) in
	x86_64 | amd64) echo amd64 ;;
	arm64 | aarch64) echo arm64 ;;
	*) err "unsupported architecture: $(uname -m)" ;;
	esac
}

resolve_tag() {
	if [ -n "${AGENT_MANAGER_VERSION:-}" ]; then
		case $AGENT_MANAGER_VERSION in
		v*) echo "$AGENT_MANAGER_VERSION" ;;
		*) echo "v${AGENT_MANAGER_VERSION}" ;;
		esac
		return
	fi
	latest_url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest") ||
		err "could not reach GitHub to resolve the latest release"
	tag=${latest_url##*/}
	case $tag in
	v*) echo "$tag" ;;
	*) err "could not resolve the latest release tag" ;;
	esac
}

fetch() {
	curl -fsSL -o "$2" "$1" || err "download failed: $1"
}

verify_checksum() {
	dir=$1
	file=$2
	expected=$(awk -v f="$file" '$2 == f {print $1}' "${dir}/checksums.txt")
	[ -n "$expected" ] || err "no checksum published for ${file}"
	if command -v sha256sum >/dev/null 2>&1; then
		actual=$(sha256sum "${dir}/${file}" | awk '{print $1}')
	elif command -v shasum >/dev/null 2>&1; then
		actual=$(shasum -a 256 "${dir}/${file}" | awk '{print $1}')
	else
		err "need sha256sum or shasum to verify the download"
	fi
	[ "$actual" = "$expected" ] || err "checksum mismatch for ${file}"
}

check_path() {
	case ":${PATH}:" in
	*":$1:"*) ;;
	*) say "add it to your PATH:  export PATH=\"$1:\$PATH\"" ;;
	esac
}

check_tmux() {
	command -v tmux >/dev/null 2>&1 && return 0
	say "${BINARY} runs its sessions in tmux, so install tmux too: apt install tmux, dnf install tmux, pacman -S tmux, or brew install tmux"
}

main() {
	need_cmd curl
	need_cmd tar
	need_cmd awk
	need_cmd mktemp

	os=$(detect_os)
	arch=$(detect_arch)
	tag=$(resolve_tag)
	version=${tag#v}

	archive="${BINARY}_${version}_${os}_${arch}.tar.gz"
	base_url="https://github.com/${REPO}/releases/download/${tag}"

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT INT TERM

	say "downloading ${BINARY} ${tag} for ${os}/${arch}"
	fetch "${base_url}/${archive}" "${tmp}/${archive}"
	fetch "${base_url}/checksums.txt" "${tmp}/checksums.txt"
	verify_checksum "$tmp" "$archive"
	tar -xzf "${tmp}/${archive}" -C "$tmp" "$BINARY"

	install_dir=${AGENT_MANAGER_INSTALL_DIR:-$HOME/.local/bin}
	mkdir -p "$install_dir" || err "could not create ${install_dir}"
	mv "${tmp}/${BINARY}" "${install_dir}/${BINARY}" || err "could not write to ${install_dir}, set AGENT_MANAGER_INSTALL_DIR to a writable directory"
	chmod 755 "${install_dir}/${BINARY}"

	say "installed $("${install_dir}/${BINARY}" --version) to ${install_dir}"
	check_path "$install_dir"
	check_tmux
}

main "$@"
