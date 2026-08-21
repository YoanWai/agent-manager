# Install

The two quickest routes are in the [README](../README.md#install): Homebrew and the install script. Every other supported route is here.

## Dependencies

| Tool | What it powers | Comes with |
| --- | --- | --- |
| tmux 3.1+ | every agent session | Homebrew, AUR, install script |
| git | diff review and worktree sessions | Homebrew (git runs Homebrew itself), AUR, install script |
| wl-clipboard, xclip, or xsel | pasting images into a prompt on Linux | your package manager |
| notify-send | desktop notifications on Linux | your desktop's notification package |

The mise, `go install`, and prebuilt-binary routes install the binary alone, so install tmux and git with your package manager. When the manager starts without tmux, or the diff view cannot find git, it names the install command it detected, or tells you to use your package manager.

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

Reads the GitHub release directly, so it needs no registry entry. Install tmux and git with your own package manager.

## Go

```bash
go install github.com/YoanWai/agent-manager@latest
```

Requires Go 1.26.5+, tmux 3.1+, and git; installs to `$(go env GOPATH)/bin`.

## Prebuilt binaries

Download from [Releases](https://github.com/YoanWai/agent-manager/releases) (macOS and Linux, amd64/arm64), and install tmux and git with your package manager.

## Windows

Run inside [WSL2](https://learn.microsoft.com/windows/wsl/install): agent-manager lives on tmux, which is a Linux/macOS tool. In a WSL shell, install with the install script, with Homebrew, or grab the Linux binary from Releases.

## Updating

The manager checks GitHub Releases every ten minutes and shows a `↑ vX.Y.Z available` badge in the header when a newer version is out. Press `u` on the update message (or `enter` on the version row in Settings) and what happens next follows the install. A Homebrew, mise, or AUR install hands the terminal to that package manager's own upgrade command, so its progress and any password prompt behave as they would in a shell. An install-script, `go install`, or manual download is updated in place, by downloading the release and swapping the binary. Either way the manager restarts into the new build with every session still running.

One case updates by hand: a pacman-owned install needs an AUR helper (`yay` or `paru`) on PATH, and without one the manager runs nothing and prints the command to use instead: `yay -S agent-manager-bin`.

The same commands work from a shell:

```bash
brew upgrade yoanwai/tap/agent-manager                                                    # Homebrew
curl -fsSL https://raw.githubusercontent.com/YoanWai/agent-manager/main/install.sh | sh   # Install script
mise upgrade --bump ubi:YoanWai/agent-manager                                             # mise
go install github.com/YoanWai/agent-manager@latest                                        # Go
```
