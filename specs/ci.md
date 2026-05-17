# Spec: CI/CD Workflows

## Overview

This specification defines the continuous integration and release automation for `evmap`. Two GitHub Actions workflows provide automated testing on every change and automated binary releases on version tags.

---

## Goals

- Run tests automatically on every push and pull request
- Prevent regressions by catching test failures before merge
- Automate release builds for multiple Linux architectures
- Generate release artifacts with checksums for verification
- Minimize manual steps in the release process
- Provide users with easy access to pre-built binaries

---

## Workflows

### 1. CI Workflow (`.github/workflows/ci.yml`)

**Trigger:** Push to any branch, or pull request to `main`

**Purpose:** Validate code quality and ensure tests pass

**Jobs:**

#### Job: `test`

**Runs on:** `ubuntu-latest`

**Go versions tested:** Current stable (1.26+)

**Steps:**
1. Checkout code
2. Set up Go environment
3. Cache Go modules
4. Download dependencies (`go mod download`)
5. Run tests (`go test -v -race ./...`)
6. Run tests with coverage (`go test -v -coverprofile=coverage.out ./...`)
7. Upload coverage report (optional: to codecov or similar)

**Exit criteria:**
- All steps must succeed
- All tests must pass
- No race conditions detected

---

### 2. Release Workflow (`.github/workflows/release.yml`)

**Trigger:** Push of a Git tag matching `v*.*.*` pattern (e.g., `v1.0.0`, `v1.2.3-rc1`)

**Purpose:** Build release binaries and create GitHub Release

**Tool:** [GoReleaser](https://goreleaser.com/)

**Jobs:**

#### Job: `release`

**Runs on:** `ubuntu-latest`

**Steps:**
1. Checkout code with full history (for changelog generation)
2. Set up Go environment
3. Run GoReleaser with GitHub token

**Outputs:**
- GitHub Release created with tag name as title
- Release notes auto-generated from commits since last tag
- Binary artifacts uploaded to release
- Checksums file uploaded to release

---

## Build Matrix

### Supported Architectures

Release builds must produce binaries for the following Linux platforms:

| OS | Architecture | GOOS | GOARCH | Binary name |
|----|--------------|------|--------|-------------|
| Linux | x86-64 (Intel/AMD) | linux | amd64 | evmap_linux_amd64 |
| Linux | ARM 64-bit | linux | arm64 | evmap_linux_arm64 |
| Linux | ARM 32-bit v7 | linux | arm | evmap_linux_armv7 |

**Rationale:**
- `amd64`: Standard desktop/laptop architecture
- `arm64`: Modern ARM servers, Raspberry Pi 4+, Apple Silicon via Linux VMs
- `armv7`: Raspberry Pi 3 and older ARM devices

**Not included (out of scope for v1.0):**
- Windows: evdev/uinput are Linux-only
- macOS: evdev/uinput are Linux-only
- 32-bit x86: legacy architecture, minimal user demand

---

## Release Artifacts

Each release must include:

### Binary Archives

For each architecture, a compressed archive containing:
- Filename format: `evmap_{version}_{os}_{arch}.tar.gz`
- Contents:
  - `evmap` binary (executable, no extension)
  - `README.md` (project documentation)
  - `LICENSE` (license file)

**Example:**
```
evmap_1.0.0_linux_amd64.tar.gz
  ├── evmap
  ├── README.md
  └── LICENSE
```

### Checksum File

- Filename: `evmap_{version}_checksums.txt`
- Format: SHA256 checksums for all archives
- One line per file: `<hash> <filename>`

**Example:**
```
a1b2c3d4... evmap_1.0.0_linux_amd64.tar.gz
e5f6g7h8... evmap_1.0.0_linux_arm64.tar.gz
i9j0k1l2... evmap_1.0.0_linux_armv7.tar.gz
```

### Release Notes

- Auto-generated from commit messages since previous tag
- Grouped by type (features, fixes, etc.) if commits follow conventional commit format
- Include installation instructions link
- Include checksums verification instructions

---

## GoReleaser Configuration (`.goreleaser.yml`)

### Build Settings

```yaml
builds:
  - env:
      - CGO_ENABLED=0  # Static binary, no C dependencies
    goos:
      - linux
    goarch:
      - amd64
      - arm64
      - arm
    goarm:
      - 7  # ARMv7 for arm32
    flags:
      - -trimpath  # Reproducible builds
    ldflags:
      - -s -w  # Strip debug info
      - -X main.version={{.Version}}  # Embed version
      - -X main.commit={{.ShortCommit}}  # Embed commit
      - -X main.date={{.Date}}  # Embed build date
```

### Archive Settings

```yaml
archives:
  - format: tar.gz
    files:
      - README.md
      - LICENSE
    name_template: "evmap_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
```

### Checksum Settings

```yaml
checksum:
  name_template: "evmap_{{ .Version }}_checksums.txt"
  algorithm: sha256
```

### Release Settings

```yaml
release:
  github:
    owner: sddev12
    name: evmap
  draft: false
  prerelease: auto  # Auto-detect pre-release from tag (e.g., v1.0.0-rc1)
```

---

## Version Embedding

The binary should report its version when run with `--version` flag (future enhancement, not required for initial CI spec).

Version information injected at build time via ldflags:
- `main.version`: Git tag (e.g., "1.0.0")
- `main.commit`: Git commit SHA (short form)
- `main.date`: Build timestamp

---

## Behaviours

### CI-01 — Tests run on every push
**Given** a developer pushes commits to any branch  
**When** the push completes  
**Then** the CI workflow triggers and runs all tests

### CI-02 — Tests run on pull requests
**Given** a pull request is opened or updated  
**When** new commits are pushed to the PR branch  
**Then** the CI workflow triggers and runs all tests

### CI-03 — Pull requests blocked if tests fail
**Given** a pull request with failing tests  
**When** a maintainer attempts to merge  
**Then** GitHub prevents the merge (requires passing checks)

### CI-04 — Race detector runs in CI
**Given** the CI workflow is running  
**When** tests execute  
**Then** the `-race` flag is used to detect race conditions

### CI-05 — Coverage report generated
**Given** the CI workflow completes successfully  
**When** tests finish  
**Then** a coverage report is generated and can be viewed

---

### REL-01 — Release triggered by version tag
**Given** a maintainer creates and pushes a version tag (e.g., `v1.0.0`)  
**When** the tag is pushed to GitHub  
**Then** the release workflow triggers automatically

### REL-02 — Release not triggered by non-version tags
**Given** a tag that doesn't match `v*.*.*` pattern (e.g., `test-tag`)  
**When** the tag is pushed  
**Then** the release workflow does not trigger

### REL-03 — Release builds for all architectures
**Given** the release workflow is running  
**When** GoReleaser executes  
**Then** binaries are built for amd64, arm64, and armv7

### REL-04 — Release creates GitHub Release
**Given** GoReleaser completes successfully  
**When** all builds finish  
**Then** a new GitHub Release is created with the tag name

### REL-05 — Release artifacts include all binaries
**Given** a GitHub Release is created  
**When** users view the release page  
**Then** tar.gz archives for all three architectures are available for download

### REL-06 — Release includes checksums file
**Given** a GitHub Release is created  
**When** users view the release page  
**Then** a checksums file with SHA256 hashes is available

### REL-07 — Release archives contain documentation
**Given** a user downloads a release archive  
**When** they extract it  
**Then** it contains the evmap binary, README.md, and LICENSE

### REL-08 — Release notes auto-generated
**Given** a GitHub Release is created  
**When** users view the release page  
**Then** release notes are populated with commit messages since the last tag

### REL-09 — Pre-release tags marked as pre-release
**Given** a tag with pre-release suffix (e.g., `v1.0.0-rc1`, `v1.0.0-beta.1`)  
**When** the release is created  
**Then** it is marked as a pre-release on GitHub

### REL-10 — Release builds are statically linked
**Given** a release binary is built  
**When** a user checks its dependencies  
**Then** it has no dynamic library dependencies (static binary)

---

## Release Process (Manual Steps)

While the CI/CD automates the build and release, the maintainer still performs these manual steps:

1. **Update version references** (if any exist in code)
2. **Commit and push changes**
3. **Create and push tag:**
   ```bash
   git tag -a v1.0.0 -m "Release v1.0.0"
   git push origin v1.0.0
   ```
4. **Monitor GitHub Actions** to ensure release workflow succeeds
5. **Verify release artifacts** on GitHub Releases page
6. **Announce release** (optional: social media, discussions, etc.)

The workflow handles everything else automatically.

---

## Future Enhancements (Out of Scope for v1.0)

- Homebrew tap for macOS/Linux package management
- Debian/RPM package builds
- Docker image builds
- AUR (Arch User Repository) package
- Automated changelog generation from conventional commits
- Code signing for binaries
- Binary verification with GPG signatures
- Performance benchmarking in CI
- Security scanning (gosec, dependabot)

---

## Dependencies

### GitHub Secrets Required

- `GITHUB_TOKEN`: Automatically provided by GitHub Actions (no manual setup needed)

### External Tools

- **GoReleaser**: Installed via GitHub Actions (no manual installation needed)
- **Go toolchain**: Provided by `actions/setup-go`

### Repository Settings

- **Branch protection** (recommended): Require CI checks to pass before merging to `main`
- **Tag protection** (recommended): Restrict tag creation to maintainers only

---

## Testing Strategy

**CI workflow testing:**
- Create a feature branch
- Push commits
- Verify CI workflow runs
- Verify tests execute and pass
- Check coverage report generation

**Release workflow testing:**
- Create a test tag (e.g., `v0.0.1-test`)
- Push to a test repository first
- Verify all architectures build successfully
- Verify archives contain correct files
- Verify checksums are accurate
- Delete test tag after verification

**Pre-release verification:**
- Use `-rc1`, `-beta.1`, or `-alpha.1` suffixes for testing
- Verify pre-release flag is set correctly
- Download and test binaries on target platforms

---

## Acceptance Criteria Summary

For the CI/CD implementation to be considered complete:

- ✅ CI workflow file exists and is valid YAML
- ✅ CI runs tests on every push and PR
- ✅ CI uses race detector
- ✅ CI generates coverage report
- ✅ Release workflow file exists and is valid YAML
- ✅ Release triggers only on version tags
- ✅ GoReleaser config exists and is valid
- ✅ Releases build for amd64, arm64, armv7
- ✅ Archives contain binary, README, LICENSE
- ✅ Checksums file is generated
- ✅ GitHub Release is created automatically
- ✅ Binaries are statically linked (CGO_ENABLED=0)
- ✅ Pre-release tags marked correctly
- ✅ All behaviour scenarios pass

---

## References

- [GitHub Actions Documentation](https://docs.github.com/en/actions)
- [GoReleaser Documentation](https://goreleaser.com/intro/)
- [Go Build Flags](https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies)
- [Semantic Versioning](https://semver.org/)
