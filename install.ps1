# Installs agent-manager on Windows:
#   irm https://raw.githubusercontent.com/YoanWai/agent-manager/main/install.ps1 | iex
# AGENT_MANAGER_VERSION pins a release, AGENT_MANAGER_INSTALL_DIR picks the
# directory (default %LOCALAPPDATA%\Programs\agent-manager).

# `irm | iex` runs in the caller's session, so everything lives in a script
# block that keeps its preferences and variables out of it, and a failure
# reports instead of closing the window.
& {
	$ErrorActionPreference = 'Stop'
	$ProgressPreference = 'SilentlyContinue'

	$Repo = 'YoanWai/agent-manager'
	$Binary = 'agent-manager.exe'

	# Windows PowerShell 5.1 does not offer TLS 1.2 by default.
	[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

	function Resolve-Tag {
		if ($env:AGENT_MANAGER_VERSION) {
			if ($env:AGENT_MANAGER_VERSION.StartsWith('v')) { return $env:AGENT_MANAGER_VERSION }
			return "v$env:AGENT_MANAGER_VERSION"
		}
		try {
			$response = Invoke-WebRequest -Uri "https://github.com/$Repo/releases/latest" -Method Head -UseBasicParsing
		} catch {
			throw 'could not reach GitHub to resolve the latest release'
		}
		# 5.1 exposes the redirect target as ResponseUri, 7 on the request message.
		$base = $response.BaseResponse
		$uri = if ($base.ResponseUri) { $base.ResponseUri } else { $base.RequestMessage.RequestUri }
		$tag = ([string]$uri).TrimEnd('/').Split('/')[-1]
		if (-not $tag.StartsWith('v')) { throw 'could not resolve the latest release tag' }
		return $tag
	}

	function Get-Arch {
		# A 32-bit PowerShell on 64-bit Windows sees x86 here; the machine's
		# own architecture is in PROCESSOR_ARCHITEW6432.
		$arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
		switch ($arch) {
			'AMD64' { return 'amd64' }
			'ARM64' { return 'arm64' }
			default { throw "unsupported architecture: $arch" }
		}
	}

	function Get-Release([string]$url, [string]$path) {
		try {
			Invoke-WebRequest -Uri $url -OutFile $path -UseBasicParsing
		} catch {
			throw "download failed: $url"
		}
	}

	function Assert-Checksum([string]$dir, [string]$file) {
		$expected = $null
		foreach ($line in Get-Content -LiteralPath (Join-Path $dir 'checksums.txt')) {
			$fields = -split $line
			if ($fields.Count -eq 2 -and $fields[1] -eq $file) { $expected = $fields[0] }
		}
		if (-not $expected) { throw "no checksum published for $file" }
		$actual = (Get-FileHash -LiteralPath (Join-Path $dir $file) -Algorithm SHA256).Hash
		if ($actual -ne $expected) { throw "checksum mismatch for $file" }
	}

	# Windows lets a running executable be renamed but not overwritten, so a
	# reinstall moves the old binary aside first.
	function Install-Binary([string]$source, [string]$dir) {
		$target = Join-Path $dir $Binary
		try {
			New-Item -ItemType Directory -Force -Path $dir | Out-Null
			$movedAside = $false
			if (Test-Path -LiteralPath $target) {
				Remove-Item -LiteralPath "$target.old" -Force -ErrorAction SilentlyContinue
				Move-Item -LiteralPath $target -Destination "$target.old" -Force
				$movedAside = $true
			}
			Copy-Item -LiteralPath $source -Destination $target -Force
		} catch {
			# A failed copy must not leave the user with no binary.
			if ($movedAside -and -not (Test-Path -LiteralPath $target)) {
				Move-Item -LiteralPath "$target.old" -Destination $target -Force -ErrorAction SilentlyContinue
			}
			throw "could not write to $dir, set AGENT_MANAGER_INSTALL_DIR to a writable directory"
		}
		return $target
	}

	function Add-UserPath([string]$dir) {
		$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
		$entries = @()
		if ($userPath) { $entries = $userPath.Split(';') | Where-Object { $_ } }
		foreach ($entry in $entries) {
			if ($entry.TrimEnd('\') -eq $dir.TrimEnd('\')) { return }
		}
		[Environment]::SetEnvironmentVariable('Path', (($entries + $dir) -join ';'), 'User')
		$env:Path = "$env:Path;$dir"
		Write-Host "added $dir to your user PATH; open a new terminal to pick it up"
	}

	function Show-MissingDep {
		$deps = @(
			@{ Command = 'psmux'; Reason = 'runs its sessions in psmux'; Install = 'winget install psmux' },
			@{ Command = 'git'; Reason = 'reviews diffs with git'; Install = 'winget install --id Git.Git -e' },
			@{ Command = 'pwsh'; Reason = 'scripts its sessions with PowerShell 7'; Install = 'winget install Microsoft.PowerShell' }
		)
		foreach ($dep in $deps) {
			if (-not (Get-Command $dep.Command -ErrorAction SilentlyContinue)) {
				Write-Host "agent-manager $($dep.Reason), which is missing; install it with: $($dep.Install)" -ForegroundColor Yellow
			}
		}
	}

	$tmp = Join-Path ([IO.Path]::GetTempPath()) ('agent-manager-install-' + [guid]::NewGuid())
	try {
		$arch = Get-Arch
		$tag = Resolve-Tag
		$version = $tag.Substring(1)
		$archive = "agent-manager_${version}_windows_${arch}.zip"
		$baseUrl = "https://github.com/$Repo/releases/download/$tag"

		New-Item -ItemType Directory -Path $tmp | Out-Null
		Write-Host "downloading agent-manager $tag for windows/$arch"
		Get-Release "$baseUrl/$archive" (Join-Path $tmp $archive)
		Get-Release "$baseUrl/checksums.txt" (Join-Path $tmp 'checksums.txt')
		Assert-Checksum $tmp $archive
		$extracted = Join-Path $tmp 'extracted'
		Expand-Archive -LiteralPath (Join-Path $tmp $archive) -DestinationPath $extracted -Force
		$source = Join-Path $extracted $Binary
		if (-not (Test-Path -LiteralPath $source)) { throw "$archive holds no $Binary" }

		$installDir = $env:AGENT_MANAGER_INSTALL_DIR
		if (-not $installDir) { $installDir = Join-Path $env:LOCALAPPDATA 'Programs\agent-manager' }
		$target = Install-Binary $source $installDir

		Write-Host "installed $(& $target --version) to $installDir"
		Add-UserPath $installDir
		Show-MissingDep
	} catch {
		Write-Host "error: $($_.Exception.Message)" -ForegroundColor Red
		$global:LASTEXITCODE = 1
		if ($PSCommandPath) { exit 1 }
	} finally {
		Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
	}
}
