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

## Homebrew cask

```bash
brew install --cask yoanwai/tap/agent-manager
```

Installs the released binary rather than building it, and pulls in tmux. The [README](../README.md#install) covers `brew install agent-manager`, the homebrew-core formula that builds from source.

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

Requires Go 1.27.1+, tmux 3.1+, and git; installs to `$(go env GOPATH)/bin`.

## Prebuilt binaries

Download from [Releases](https://github.com/YoanWai/agent-manager/releases) (macOS, Linux and Windows, amd64/arm64), and install tmux (psmux on Windows) and git with your package manager.

## Windows

agent-manager runs natively on Windows on [psmux](https://github.com/psmux/psmux), a tmux-compatible multiplexer, with PowerShell as the pane shell. In PowerShell:

```powershell
irm https://raw.githubusercontent.com/YoanWai/agent-manager/main/install.ps1 | iex
```

The script installs `agent-manager.exe` to `%LOCALAPPDATA%\Programs\agent-manager` (or `$env:AGENT_MANAGER_INSTALL_DIR`), adds that directory to your user `Path`, and names whatever is missing of its dependencies:

| Tool | Install |
| --- | --- |
| psmux | `winget install psmux` (or `cargo install psmux`) |
| git | `winget install --id Git.Git -e` |
| PowerShell 7 | `winget install Microsoft.PowerShell` (Windows PowerShell 5.1 works, but can mangle quotes in arguments to agent CLIs) |

Differences from macOS and Linux:

- Each session runs on its own psmux server. Previews poll the pane rather than streaming it, since the manager does not use psmux's control mode.
- Inside an attached session, the review and editor keys detach through a short PowerShell `run-shell`, so they take a moment longer than on tmux.
- Command Code (`cmd`) shares its name with `cmd.exe` and is not supported natively.

agent-manager also still runs inside [WSL2](https://learn.microsoft.com/windows/wsl/install), on tmux: in a WSL shell, install with the install script, with Homebrew, or grab the Linux binary from Releases. The agent CLIs then belong in the distro too. WSL appends the Windows `PATH` to the distro's, so a CLI installed on the Windows side is visible in a WSL shell, and running it there starts a Windows process or fails without a Linux runtime. A spawn finding only that copy stops on the setup dialog, which names where the Windows copy is and the command that installs the CLI in the distro.

## Updating

The manager checks GitHub Releases every ten minutes and shows a `↑ vX.Y.Z available` badge in the header when a newer version is out. Press `u` on the update message (or `enter` on the version row in Settings) and what happens next follows the install. A Homebrew, mise, or AUR install hands the terminal to that package manager's own upgrade command, so its progress and any password prompt behave as they would in a shell. An install-script, `go install`, or manual download is updated in place, by downloading the release and swapping the binary. Either way the manager restarts into the new build with every session still running.

Two cases update by hand. A pacman-owned install needs an AUR helper (`yay` or `paru`) on PATH, and without one the manager runs nothing and prints the command to use instead: `yay -S agent-manager-bin`. A Nix install lives in the read-only store, so the manager runs nothing and points you at Nix instead, such as `nix profile upgrade agent-manager` or your flake or NixOS config.

The same commands work from a shell:

```bash
brew upgrade agent-manager                                                                # Homebrew formula
brew upgrade --cask yoanwai/tap/agent-manager                                             # Homebrew cask
curl -fsSL https://raw.githubusercontent.com/YoanWai/agent-manager/main/install.sh | sh   # Install script
mise upgrade --bump ubi:YoanWai/agent-manager                                             # mise
go install github.com/YoanWai/agent-manager@latest                                        # Go
```

Later, `agent-manager update` brings the binary to the newest release without re-running any of these: it hands the update to the package manager that installed it, or downloads and swaps the binary itself.
