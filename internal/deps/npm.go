package deps

import (
	"fmt"
	"strings"
)

// Installed binaries must be visible on the manager's inherited PATH.
const shellHelpers = `
on_windows_path() {
  target=$1
  case $target in
    *.exe|*.cmd|*.bat) return 0 ;;
  esac
  case $target in
    /mnt/*)
      dir=$(dirname "$target")
      if [ -e "$dir/node.exe" ] || [ -e "$dir/npm.cmd" ] || [ -e "$dir/opencode.exe" ]; then
        return 0
      fi
      ;;
  esac
  if command -v findmnt >/dev/null 2>&1; then
    info=$(findmnt -n -o FSTYPE,OPTIONS -T "$target" 2>/dev/null || true)
    fstype=${info%% *}
    case $fstype in
      drvfs|virtiofs) return 0 ;;
      9p)
        # A 9p mount is a Windows drive only when it carries the drvfs attach name.
        case $info in
          *aname=drvfs*) return 0 ;;
        esac
        ;;
    esac
  fi
  return 1
}
visible_bin() {
  name=$1
  saved=$2
  found=$(PATH=$saved command -v "$name" 2>/dev/null) || return 1
  on_windows_path "$found" && return 1
  return 0
}
usable_npm() {
  npm_path=$(command -v npm 2>/dev/null) || return 1
  on_windows_path "$npm_path" && return 1
  return 0
}
writable_dir() {
  check=$1
  while [ ! -e "$check" ]; do
    parent=${check%/*}
    [ -n "$parent" ] && [ "$parent" != "$check" ] || return 1
    check=$parent
  done
  [ -d "$check" ] && [ -w "$check" ]
}
first_writable() {
  saved=$1
  rest=$saved
  while [ -n "$rest" ]; do
    case $rest in
      *:*) entry=${rest%%:*}; rest=${rest#*:} ;;
      *) entry=$rest; rest= ;;
    esac
    [ -n "$entry" ] && [ "$entry" != . ] || continue
    case $entry in
      /mnt/*) continue ;;
    esac
    [ -d "$entry" ] && [ -w "$entry" ] || continue
    printf '%s\n' "$entry"
    return 0
  done
  return 1
}
publish_file() {
  file=$1
  saved=$2
  [ -e "$file" ] || return 1
  name=$(basename "$file")
  if visible_bin "$name" "$saved"; then
    return 0
  fi
  dest=$(first_writable "$saved" || true)
  if [ -z "$dest" ]; then
    dest=/usr/local/bin
    if [ "$(id -u)" -eq 0 ]; then
      mkdir -p "$dest" || return 1
      ln -sfn "$file" "$dest/$name"
      return
    fi
    command -v sudo >/dev/null 2>&1 || return 1
    sudo mkdir -p "$dest" || return 1
    sudo ln -sfn "$file" "$dest/$name"
    return
  fi
  ln -sfn "$file" "$dest/$name"
}
install_node() {
  case $(uname -s) in
    Darwin) platform=darwin ;;
    Linux) platform=linux ;;
    *) printf 'unsupported operating system for Node.js\n' >&2; return 1 ;;
  esac
  case $(uname -m) in
    x86_64|amd64) cpu=x64 ;;
    aarch64|arm64) cpu=arm64 ;;
    *) printf 'unsupported architecture for Node.js\n' >&2; return 1 ;;
  esac
  libc=
  if [ "$platform" = linux ]; then
    if [ -f /etc/alpine-release ] || ldd /bin/sh 2>&1 | grep -q musl; then
      libc=-musl
    fi
  fi
  version=$(curl -fsSL https://nodejs.org/dist/index.json | tr '{' '\n' | awk -F'"' '
    $0 ~ /"lts":"[^"]+"/ {
      for (i = 1; i < NF; i++) if ($i == "version") { print $(i+2); exit }
    }
  ') || return 1
  [ -n "$version" ] || return 1
  suffix="-$platform-$cpu$libc.tar.gz"
  base="https://nodejs.org/dist/$version"
  tmp=$(mktemp -d) || return 1
  curl -fsSL "$base/SHASUMS256.txt" -o "$tmp/SHASUMS256.txt" || {
    rm -rf "$tmp"
    return 1
  }
  file=$(awk -v suffix="$suffix" '
    index($2, "node-v") == 1 && length($2) >= length(suffix) && substr($2, length($2) - length(suffix) + 1) == suffix { print $2; exit }
  ' "$tmp/SHASUMS256.txt")
  if [ -z "$file" ]; then
    rm -rf "$tmp"
    printf 'no Node.js build for %s-%s%s; install nodejs and npm with your package manager, then spawn again\n' "$platform" "$cpu" "$libc" >&2
    return 1
  fi
  curl -fsSL "$base/$file" -o "$tmp/$file" || {
    rm -rf "$tmp"
    return 1
  }
  awk -v wanted="$file" '$2 == wanted { print }' "$tmp/SHASUMS256.txt" > "$tmp/check"
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$tmp" && sha256sum -c check) || {
      rm -rf "$tmp"
      return 1
    }
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$tmp" && shasum -a 256 -c check) || {
      rm -rf "$tmp"
      return 1
    }
  else
    rm -rf "$tmp"
    printf 'need sha256sum or shasum to verify Node.js\n' >&2
    return 1
  fi
  tar -xzf "$tmp/$file" -C "$tmp" || {
    rm -rf "$tmp"
    return 1
  }
  extracted=
  for dir in "$tmp"/node-*; do
    [ -d "$dir" ] || continue
    extracted=$dir
    break
  done
  if [ -z "$extracted" ]; then
    rm -rf "$tmp"
    return 1
  fi
  mkdir -p "$HOME/.local" || {
    rm -rf "$tmp"
    return 1
  }
  tar -cf - -C "$extracted" bin lib | tar -xf - -C "$HOME/.local" || {
    rm -rf "$tmp"
    return 1
  }
  rm -rf "$tmp"
}
ensure_npm() {
  if usable_npm; then
    return 0
  fi
  install_node || return 1
  PATH="$HOME/.local/bin:$PATH"
  export PATH
  usable_npm
}
`

const unixNpmBody = `
saved_path=$PATH
ensure_npm || exit 1
prefix=$(npm prefix -g 2>/dev/null || true)
if [ -n "$prefix" ] && writable_dir "$prefix/lib/node_modules" && writable_dir "$prefix/bin"; then
  npm install -g "$pkg" || exit 1
  bindir=$prefix/bin
else
  mkdir -p "$HOME/.local/bin" || exit 1
  npm install -g --prefix "$HOME/.local" "$pkg" || exit 1
  bindir=$HOME/.local/bin
fi
publish_file "$bindir/$bin" "$saved_path" || exit 1
if ! visible_bin node "$saved_path"; then
  if [ -x "$bindir/node" ] || [ -L "$bindir/node" ]; then
    publish_file "$bindir/node" "$saved_path" || exit 1
  elif [ -x "$HOME/.local/bin/node" ] || [ -L "$HOME/.local/bin/node" ]; then
    publish_file "$HOME/.local/bin/node" "$saved_path" || exit 1
  else
    exit 1
  fi
fi
visible_bin "$bin" "$saved_path" || exit 1
`

func unixNpm(bin, pkg string) string {
	body := strings.ReplaceAll(strings.TrimPrefix(unixNpmBody, "\n"), "\"$pkg\"", pkg)
	return fmt.Sprintf("bin='%s'\ninstall_cli() {\n%s}\n", bin, body) + shellHelpers + "install_cli\n"
}

func unixOpencode() string {
	return shellHelpers + `
saved_path=$PATH
curl -fsSL https://opencode.ai/v2/install | bash -s -- --no-modify-path || exit 1
installed="$HOME/.opencode/bin/opencode"
[ -x "$installed" ] || exit 1
publish_file "$installed" "$saved_path" || exit 1
visible_bin opencode "$saved_path" || exit 1
`
}

// The Windows global prefix is the user profile, which a normal account can write.
func windowsNpm(args string) string {
	return `if (-not (Get-Command npm -ErrorAction SilentlyContinue)) { winget install --id OpenJS.NodeJS.LTS -e --accept-package-agreements --accept-source-agreements; $env:Path = [Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [Environment]::GetEnvironmentVariable('Path','User') }; npm install -g ` + args
}
