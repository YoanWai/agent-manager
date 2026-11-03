# Install

The quickest routes are in the [README](../README.md#install): Homebrew, Scoop, and the install script. Every other supported route is here.

## Dependencies

| Tool | What it powers | Comes with |
| --- | --- | --- |
| tmux 3.1+ | every agent session | Homebrew, AUR, install script |
| git | diff review and worktree sessions | Homebrew (git runs Homebrew itself), AUR, install script |
| wl-clipboard or xclip | pasting images into a prompt on Linux | the install script offers one of them on a Linux desktop, or your package manager |
| notify-send | desktop notifications on Linux | the install script on a Linux desktop (`libnotify-bin` with apt, `libnotify-tools` with zypper, `libnotify` with the others), or your package manager |

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
mise use -g ubi:wainmann/agent-manager
```

Reads the GitHub release directly, so it needs no registry entry. Install tmux and git with your own package manager.

## Go

```bash
go install github.com/YoanWai/agent-manager@latest
```

Requires Go 1.27.3+, tmux 3.1+, and git; installs to `$(go env GOPATH)/bin`.

## Prebuilt binaries

Download from [Releases](https://github.com/wainmann/agent-manager/releases) (macOS, Linux and Windows, amd64/arm64), and install tmux (psmux on Windows) and git with your package manager.

## Windows

agent-manager runs natively on Windows on [psmux](https://github.com/psmux/psmux) 3.4.0 or newer, a tmux-compatible multiplexer, with PowerShell as the pane shell.

### Scoop

```powershell
scoop bucket add yoanwai https://github.com/YoanWai/scoop-bucket
scoop install yoanwai/agent-manager
```

Installs the version published in [YoanWai/scoop-bucket](https://github.com/YoanWai/scoop-bucket), and Scoop installs psmux from the main bucket. The bucket is updated on its own, so it can lag the latest release. The zip and install script below install the current release. Install git and PowerShell 7 with `scoop install git pwsh` if they are missing.

<!--
winget is not listed yet. Each release opens a manifest PR to
microsoft/winget-pkgs (the winget block in .goreleaser.yaml), and
microsoft/winget-pkgs has no YoanWai.AgentManager until one merges.
Open on 2026-11-03: microsoft/winget-pkgs#453457 (new package, 0.43.1)
and microsoft/winget-pkgs#469116 (0.47.1).
When the package is published, add:
winget install --id YoanWai.AgentManager -e
-->

### Download and verify the zip

For a fresh install, run these commands in PowerShell on Windows from an empty working directory. They select the native architecture, download the zip and `checksums.txt` from one release, and stop if the SHA-256 does not match. The checksum checks the download against that release's published digest. The optional attestation check below also verifies its build provenance.

```powershell
$ErrorActionPreference = 'Stop'
$release = Invoke-RestMethod https://api.github.com/repos/wainmann/agent-manager/releases/latest
$tag = $release.tag_name
$version = $tag.TrimStart('v')
$machineArch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
$arch = switch ($machineArch) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported architecture $machineArch" }
}
$archive = "agent-manager_${version}_windows_${arch}.zip"
$baseUrl = "https://github.com/wainmann/agent-manager/releases/download/$tag"
Invoke-WebRequest "$baseUrl/$archive" -OutFile $archive -UseBasicParsing
Invoke-WebRequest "$baseUrl/checksums.txt" -OutFile checksums.txt -UseBasicParsing
$checksum = @(Get-Content .\checksums.txt | Where-Object {
    $_ -match "^[0-9a-fA-F]{64}\s+$([regex]::Escape($archive))$"
})
if ($checksum.Count -ne 1) { throw "Expected one checksum for $archive" }
$expected = ($checksum[0] -split '\s+')[0]
$actual = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash
if ($actual -ne $expected) { throw "Checksum mismatch for $archive" }
```

Optionally verify provenance before extracting. This needs a logged-in GitHub CLI with `gh attestation` support. A missing `gh` stops the example. PowerShell would otherwise keep the previous `$LASTEXITCODE`, and a `0` from an earlier command would look like a successful verify. Releases before v0.47.1 verify with `--owner YoanWai --signer-repo YoanWai/agent-manager` in place of `--repo wainmann/agent-manager`.

```powershell
Get-Command gh -ErrorAction Stop | Out-Null
if ($?) {
    gh attestation verify $archive --repo wainmann/agent-manager
    if ($LASTEXITCODE -ne 0) { throw 'Attestation verification failed' }
}
```

After verification, extract into `%LOCALAPPDATA%\Programs\agent-manager`, add it to your user `Path` and this terminal's `Path`, and install the dependencies. These winget commands install psmux, git and PowerShell 7 (`pwsh`).

```powershell
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\agent-manager'
Expand-Archive -LiteralPath $archive -DestinationPath $installDir -Force
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$entries = @($userPath -split ';' | Where-Object { $_ })
if ($installDir -notin $entries) {
    [Environment]::SetEnvironmentVariable('Path', (($entries + $installDir) -join ';'), 'User')
}
$env:Path = "$installDir;$env:Path"
winget install --id marlocarlo.psmux -e
winget install --id Git.Git -e
winget install --id Microsoft.PowerShell -e
```

The dependency versions and their purpose are listed below. Open a new terminal and run `agent-manager` after installation.

### Install script

Download the script, read it, then run it. The installer runs only after that download succeeds. Windows PowerShell 5.1 has no `&&`, so the guard is `if ($?)`. Until the release attaches `install.ps1`, the file to read is the copy on `main`. That copy is not in `checksums.txt`.

```powershell
irm https://raw.githubusercontent.com/wainmann/agent-manager/main/install.ps1 -OutFile install.ps1
if ($?) {
    Get-Content .\install.ps1
    Unblock-File .\install.ps1
    powershell -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
}
```

Once the release attaches `install.ps1` and lists it in `checksums.txt`, download that file instead. Logged in to `gh`, verify it after the download and before the installer. Releases before v0.47.1 verify with `--owner YoanWai --signer-repo YoanWai/agent-manager` in place of `--repo wainmann/agent-manager`. A missing `gh` stops the example. PowerShell would otherwise keep the previous `$LASTEXITCODE`, and a `0` from an earlier command would look like a successful verify. That release URL has no file until that tag exists.

```powershell
irm https://github.com/wainmann/agent-manager/releases/latest/download/install.ps1 -OutFile install.ps1
if ($?) {
    Get-Command gh -ErrorAction Stop | Out-Null
    if ($?) {
        gh attestation verify .\install.ps1 --repo wainmann/agent-manager
        if ($LASTEXITCODE -ne 0) { throw 'attestation verification failed' }
        if ($LASTEXITCODE -eq 0) {
            Get-Content .\install.ps1
            Unblock-File .\install.ps1
            powershell -NoProfile -ExecutionPolicy Bypass -File .\install.ps1
        }
    }
}
```

On a Windows client with no execution policy set, the effective policy is Restricted, so every `.ps1` is refused. `-ExecutionPolicy Bypass` applies to that process only. Where the policy is already RemoteSigned, `Unblock-File` then `.\install.ps1` is enough. RemoteSigned refuses an unsigned script that still has the Mark of the Web. A browser download sets that mark. `irm -OutFile` does not, because Microsoft documents `Invoke-WebRequest` as not marking the Internet zone. `Unblock-File` does nothing when the mark is absent.

The script installs `agent-manager.exe` to `%LOCALAPPDATA%\Programs\agent-manager` (or `$env:AGENT_MANAGER_INSTALL_DIR`), adds that directory to your user `Path`, and offers to install whatever is missing of its dependencies. `AGENT_MANAGER_INSTALL_DEPS=1` answers yes. `AGENT_MANAGER_INSTALL_DEPS=0` skips the prompt. With no terminal it skips the prompt and still leaves the binary installed.

Short form. Piping into `iex` skips the download-and-read step and does not consult the execution policy.

```powershell
irm https://raw.githubusercontent.com/wainmann/agent-manager/main/install.ps1 | iex
```

On macOS and Linux the same choice, until the release attaches `install.sh`, is `curl -fsSL -o install.sh https://raw.githubusercontent.com/wainmann/agent-manager/main/install.sh && sh install.sh`. That copy is not in `checksums.txt`. Once the release attaches it, the checksummed file is `https://github.com/wainmann/agent-manager/releases/latest/download/install.sh`, and `gh attestation verify install.sh --repo wainmann/agent-manager` belongs between the download and `sh install.sh`. Releases before v0.47.1 verify with `--owner YoanWai --signer-repo YoanWai/agent-manager` in place of `--repo wainmann/agent-manager`. The short form, with nothing saved to read, is `curl -fsSL https://raw.githubusercontent.com/wainmann/agent-manager/main/install.sh | sh`.

| Tool | Install |
| --- | --- |
| psmux | `winget install --id marlocarlo.psmux -e` (or `cargo install psmux`). 3.4.0 or newer. Older builds misbehave and the manager refuses them |
| git | `winget install --id Git.Git -e` |
| PowerShell 7.3 or newer (`pwsh`) | `winget install --id Microsoft.PowerShell -e`. The manager refuses Windows PowerShell 5.1 and PowerShell before 7.3, because they split a prompt containing `"` into several arguments to the agent CLI |

Differences from macOS and Linux:

- Each session runs on its own psmux server. The manager does not use psmux's control mode, so previews poll the pane rather than streaming it, and a focused session is captured again as each typed key lands. psmux does not report whether a program hides its cursor, so the focused view draws the caret wherever the pane's cursor sits.
- Inside an attached session, the review and editor keys detach through a short PowerShell `run-shell`, so they take a moment longer than on tmux.
- Command Code (`cmd`) shares its name with `cmd.exe`, so it is not supported natively: it is left out of the tool pickers, and a session asking for it is refused.
- An agent CLI installed only as a `.cmd` or `.bat` file is refused, since cmd.exe would run a prompt holding `"` as commands. CLIs installed with npm work, because npm adds a `.ps1` beside each `.cmd`.
- A CLI the setup dialog installs with npm lands in `%APPDATA%\npm`. When that directory was not on `Path` before the manager started, the manager keeps its old `Path`, so start it again before spawning that CLI.

agent-manager also still runs inside [WSL2](https://learn.microsoft.com/windows/wsl/install), on tmux: in a WSL shell, install with the install script, with Homebrew, or grab the Linux binary from Releases. The agent CLIs then belong in the distro too. WSL appends the Windows `PATH` to the distro's, so a CLI installed on the Windows side is visible in a WSL shell, and running it there starts a Windows process or fails without a Linux runtime. A spawn finding only that copy stops on the setup dialog, which names where the Windows copy is and the command that installs the CLI in the distro. The install script skips Linux clipboard and notification packages under WSL. Paste and toasts there go through the Windows host.

## Checking a download

The release workflow attests every digest in `checksums.txt`. That covers the archives, and, from the release that attaches them, `install.ps1` and `install.sh`. `gh` must be logged in, and it has to be a build that provides `gh attestation`. The current upstream release does. An anonymous call stops at `gh auth login`, and the attestation API is rate-limited like the rest of GitHub's API.

```bash
gh attestation verify <archive> --repo wainmann/agent-manager
```

`<archive>` is the file you downloaded, such as `agent-manager_0.47.1_linux_amd64.tar.gz`. The same command checks `install.ps1` or `install.sh` once that release attaches them. Releases before v0.47.1 verify with `--owner YoanWai --signer-repo YoanWai/agent-manager` in place of `--repo wainmann/agent-manager`.

The install scripts run this check only when `AGENT_MANAGER_VERIFY_ATTESTATION=1` (also `y`, `yes`, or `true`, in any case). They try `--repo wainmann/agent-manager` first, which releases from v0.47.1 on pass. When that fails they retry with `--owner YoanWai --signer-repo YoanWai/agent-manager`, which earlier releases pass, and they stop only if both fail. They skip it when the variable is unset, including on a machine with no `gh`. `gh` uses one exit status for a rejected attestation, a missing login, and a network error. The script treats a failure of both as fatal when the variable is set, because guessing from the error text would be wrong. The check can be queried once the release workflow's attest step has finished.

## Updating

The manager checks GitHub Releases every ten minutes and shows a `↑ vX.Y.Z available` badge when a newer version is out. The badge sits in the header. With the header hidden, the same badge sits in the top edge of the frame. Press `u` on the update message (or `enter` on the version row in Settings) and what happens next follows the install. A Homebrew, mise, or AUR install hands the terminal to that package manager's own upgrade command, so its progress and any password prompt behave as they would in a shell. An install-script, `go install`, or manual download is updated in place, by downloading the release and swapping the binary. After a successful update the manager restarts into the new build with every session still running.

Scoop refuses to update a running app. For a Scoop install, the manager shows instructions instead of replacing the binary or running an update that Scoop would skip. Close agent-manager, run `scoop update agent-manager`, then start it again. For a global install, use `scoop update agent-manager --global` in an elevated PowerShell. Agent sessions keep running while the manager is closed.

Two cases update by hand. A pacman-owned install needs an AUR helper (`yay` or `paru`) on PATH, and without one the manager runs nothing and prints the command to use instead: `yay -S agent-manager-bin`. A Nix install lives in the read-only store, so the manager runs nothing and points you at Nix instead, such as `nix profile upgrade agent-manager` or your flake or NixOS config.

The same commands work from a shell:

```bash
brew upgrade agent-manager                                                                # Homebrew formula
brew upgrade --cask yoanwai/tap/agent-manager                                             # Homebrew cask
scoop update agent-manager                                                               # Scoop, after closing agent-manager
curl -fsSL -o install.sh https://raw.githubusercontent.com/wainmann/agent-manager/main/install.sh && sh install.sh
mise upgrade --bump ubi:wainmann/agent-manager                                            # mise
mise upgrade --bump ubi:YoanWai/agent-manager                                             # mise, earlier installs
go install github.com/YoanWai/agent-manager@latest                                        # Go
```

The install script line reads `install.sh` from `main`. The release that first attaches the script lists it in `checksums.txt` at `https://github.com/wainmann/agent-manager/releases/latest/download/install.sh`. Until that tag exists, the line above is the one that works.

Later, `agent-manager update` brings the binary to the newest release without re-running any of these: it hands the update to the package manager that installed it, or downloads and swaps the binary itself.

## Uninstalling

Agent sessions keep running on their own server after the manager quits. Stop them first with `X` in the manager, which kills every live session in view. On macOS and Linux, `tmux -L agentmgr kill-server` ends all of them at once. It touches only the manager's private `agentmgr` server, never your own tmux.

Then remove the binary with the route that installed it:

```bash
brew uninstall --formula agent-manager                    # Homebrew formula
brew uninstall --cask agent-manager                       # Homebrew cask
brew untap yoanwai/tap                                    # Homebrew cask, once nothing else uses the tap
sudo pacman -Rs agent-manager-bin                         # Arch Linux (AUR)
mise unuse -g ubi:wainmann/agent-manager                  # mise, removes it from the global config
mise uninstall --all ubi:wainmann/agent-manager           # mise, removes the installed versions
rm "$(go env GOPATH)/bin/agent-manager"                   # Go, or "$(go env GOBIN)/agent-manager" when GOBIN is set
rm ~/.local/bin/agent-manager                             # install script, or the AGENT_MANAGER_INSTALL_DIR you set
```

For mise, use the backend and slug you installed with. `mise ls` lists them, and an earlier install reads `ubi:YoanWai/agent-manager`. A prebuilt binary from Releases is removed by deleting the file wherever you put it.

On Windows, close agent-manager first. For Scoop:

```powershell
scoop uninstall agent-manager
scoop bucket rm yoanwai
```

For the install script or the zip, delete the install directory and take it off your user `Path`. Use `$env:AGENT_MANAGER_INSTALL_DIR` in place of the default when you set it for the install script.

```powershell
$installDir = Join-Path $env:LOCALAPPDATA 'Programs\agent-manager'
Remove-Item -LiteralPath $installDir -Recurse -Force
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
$entries = @($userPath -split ';' | Where-Object { $_ -and $_.TrimEnd('\') -ne $installDir.TrimEnd('\') })
[Environment]::SetEnvironmentVariable('Path', ($entries -join ';'), 'User')
```

### Settings and state

Removing the binary keeps your settings, sessions and caches. They live in one directory. It is `AGENT_MANAGER_CONFIG_DIR` when that is set, otherwise `agent-manager` under `XDG_CONFIG_HOME` when that is set, otherwise `agent-manager` under your OS user config directory. It holds `state.db`, the release and message caches, and the generated hook and MCP files the launches load. On macOS it also holds the notification helper app. Delete it to remove everything:

```bash
rm -rf ~/Library/Application\ Support/agent-manager        # macOS
rm -rf "${XDG_CONFIG_HOME:-$HOME/.config}/agent-manager"    # Linux
```

```powershell
Remove-Item -LiteralPath (Join-Path $env:APPDATA 'agent-manager') -Recurse -Force
```

On macOS the notification sounds are copied to `~/Library/Sounds` as `agent-manager-*.aiff`. Remove them with `rm ~/Library/Sounds/agent-manager-*.aiff`.

Some agent CLIs keep an `agent-manager` MCP server entry in their own user config, which the manager adds when it first launches them. Grok, Gemini CLI, Command Code, Antigravity and Hermes register it through their own `mcp add` command, so remove it with that CLI's MCP commands. Muse Code keeps it in `muse/settings.json` under `XDG_CONFIG_HOME` (`~/.config` by default), and Cursor CLI in `~/.cursor/mcp.json`. Delete the `agent-manager` entry from those files. The other CLIs load the manager's files from its own directory on each launch, so deleting that directory removes them.
