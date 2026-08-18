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

tmux_new_enough() {
	command -v tmux >/dev/null 2>&1 || return 1
	out=$(tmux -V 2>/dev/null) || return 1
	ver=${out#tmux }
	ver=${ver%%[a-zA-Z]*}
	major=${ver%%.*}
	rest=${ver#*.}
	minor=${rest%%.*}
	[ -n "$major" ] && [ -n "$minor" ] || return 1
	[ "$major" -gt 3 ] && return 0
	[ "$major" -eq 3 ] && [ "$minor" -ge 1 ]
}

missing_deps() {
	missing=
	tmux_new_enough || missing="${missing} tmux"
	command -v git >/dev/null 2>&1 || missing="${missing} git"
	printf '%s' "${missing# }"
}

# A command substitution would strip the trailing space, so the prefix lands in
# a variable. Homebrew refuses to run under sudo, so its branch leaves it off.
SUDO=''
set_sudo() {
	if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1; then
		SUDO='sudo '
	fi
}

# Native package managers come before brew on Linux, where a Homebrew install
# is the exception; on macOS brew is the only one of these that ships packages.
dep_install_cmd() {
	packages=$1
	if [ "$(detect_os)" = darwin ]; then
		command -v brew >/dev/null 2>&1 || return 1
		printf 'brew install %s' "$packages"
		return 0
	fi
	set_sudo
	for candidate in apt-get dnf pacman zypper apk brew; do
		command -v "$candidate" >/dev/null 2>&1 || continue
		if [ "$candidate" != brew ] && [ "$(id -u)" -ne 0 ] && [ -z "$SUDO" ]; then
			continue
		fi
		case $candidate in
		apt-get) printf '%sapt-get update && %sapt-get install -y %s' "$SUDO" "$SUDO" "$packages" ;;
		dnf) printf '%sdnf install -y %s' "$SUDO" "$packages" ;;
		pacman) printf '%spacman -S --needed --noconfirm %s' "$SUDO" "$packages" ;;
		zypper) printf '%szypper install -y %s' "$SUDO" "$packages" ;;
		apk) printf '%sapk add %s' "$SUDO" "$packages" ;;
		brew) printf 'brew install %s' "$packages" ;;
		esac
		return 0
	done
	return 1
}

# A `test -r` passes on a /dev/tty that has no controlling terminal behind it,
# so the probe opens the device instead.
have_tty() {
	{ : >/dev/tty; } 2>/dev/null
}

# The script itself occupies stdin under `curl | sh`, so the prompt reads the
# terminal directly and treats an unreachable one as a decline.
confirm() {
	case ${AGENT_MANAGER_INSTALL_DEPS:-} in
	1 | y | yes | true) return 0 ;;
	0 | n | no | false) return 1 ;;
	esac
	have_tty || return 1
	printf '%s [Y/n] ' "$1" >/dev/tty
	read -r reply </dev/tty || return 1
	case $reply in
	'' | y | Y | yes | Yes) return 0 ;;
	*) return 1 ;;
	esac
}

install_deps() {
	missing=$(missing_deps)
	[ -n "$missing" ] || return 0

	say "${BINARY} runs its sessions in tmux and reviews diffs with git; missing: ${missing}"
	if ! cmd=$(dep_install_cmd "$missing"); then
		say "install ${missing} with your package manager"
		return 0
	fi
	if ! confirm "install ${missing} now with \`${cmd}\`?"; then
		say "install ${missing} yourself: ${cmd}"
		return 0
	fi
	say "running: ${cmd}"
	# The pipe feeding this script is still on stdin, so a package manager
	# that reads it would swallow the rest of the script.
	stdin=/dev/null
	have_tty && stdin=/dev/tty
	if ! sh -c "$cmd" <"$stdin"; then
		say "that failed, run it yourself: ${cmd}"
		return 0
	fi
	remaining=$(missing_deps)
	if [ -n "$remaining" ]; then
		say "installed packages do not satisfy: ${remaining}"
	fi
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
	install_deps
}

main "$@"
