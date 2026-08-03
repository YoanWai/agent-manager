# Install

The two quickest routes are in the [README](../README.md#install): Homebrew and the install script. Every other supported route is here.

## Arch Linux

```bash
yay -S agent-manager-bin
```

[![AUR version](https://img.shields.io/aur/version/agent-manager-bin?style=flat-square&logo=archlinux&logoColor=white&label=aur&color=6f9fd0)](https://aur.archlinux.org/packages/agent-manager-bin)

[agent-manager-bin](https://aur.archlinux.org/packages/agent-manager-bin) in the AUR installs the released binary and pulls in tmux and git.

## mise

```bash
mise use -g ubi:YoanWai/agent-manager
```

Reads the GitHub release directly, so it needs no registry entry. Install tmux with your own package manager.

## Go

```bash
go install github.com/YoanWai/agent-manager@latest
```

Requires Go 1.26.5+ and tmux 3.1+; installs to `$(go env GOPATH)/bin`.

## Prebuilt binaries

Download from [Releases](https://github.com/YoanWai/agent-manager/releases) (macOS and Linux, amd64/arm64).

## Windows

Run inside [WSL2](https://learn.microsoft.com/windows/wsl/install): agent-manager lives on tmux, which is a Linux/macOS tool. In a WSL shell, install with the install script, with Homebrew, or grab the Linux binary from Releases.

## Updating

The manager checks GitHub Releases once a day and shows a `↑ vX.Y.Z available` badge in the header when a newer version is out. Press `u` on the update message (or `enter` on the version row in Settings) and the manager updates itself whatever the install source: a Homebrew, mise, or AUR install hands the terminal to that package manager's upgrade command, a direct install downloads the release and swaps the binary in place, and either way the manager restarts into the new build with every session intact.

The same commands work from a shell:

```bash
brew upgrade yoanwai/tap/agent-manager                                                    # Homebrew
curl -fsSL https://raw.githubusercontent.com/YoanWai/agent-manager/main/install.sh | sh   # Install script
mise upgrade --bump ubi:YoanWai/agent-manager                                             # mise
go install github.com/YoanWai/agent-manager@latest                                        # Go
```
