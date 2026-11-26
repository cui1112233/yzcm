# 临时 macOS 局域网运行包实施计划

> For agentic workers: use superpowers:executing-plans task-by-task. User approved the temporary local deployment and specified an M4 Mac; maintain the existing v88 branch.

**Goal:** Deliver a private Mac package with the current embedded Go application and a snapshot of online accounts, team permissions and necessary configuration. Retain original passwords without extracting plaintext passwords.

**Architecture:** The Mac runs its own loopback-only MySQL 8.4 and Redis, with Go serving port 4001 to the LAN. Homebrew provides native database, queue and FFmpeg executables; package scripts own separate data directories and ports. Online execution jobs, browser sessions and generated works are excluded. New local work stays in the package's runtime folder for later migration; it does not automatically sync online.

**Tech Stack:** Go embed, macOS arm64/amd64, Bash 3.2, MySQL 8.4, Redis, FFmpeg. Python is only a build/export tool on the operator's computers, never a Mac runtime dependency.

## Global constraints

- Do not change or restart the current local 4001 or production application.
- Snapshot production read-only; no paid model calls during acceptance.
- Private credentials and password hashes stay in ignored local artifacts, never in Git or public web assets. Do not ship production database passwords, SSH keys, auth sessions or live task records.
- Expose only application HTTP to LAN; database and Redis bind 127.0.0.1 with fresh package-local credentials and distinct ports.
- Verify native Mach-O binaries and identical frontend entry/hash. Actual execution on the user's Mac remains a separate acceptance step if the Mac is unavailable.
- Local and online Redis are separate: local instance policy/config is independent, no global concurrency guarantee across installations. README must direct operators to place new work on one entry point during temporary use; no production queue pause is performed here.

## Task 1 — Snapshot and bootstrap fixtures

Files: scripts/lan-macos/export_snapshot.py, scripts/lan-macos/test_package.py.

- [x] Test allowlisted data tables/files, exclusion of sessions/jobs and environment path remapping.
- [x] Export full empty schema and migration ledger, and only account/config/preset table data; copy allowlisted encrypted per-user API files with their existing decryption material, team/MFA files, avatars and configured connector env settings. Never print secrets.
- [x] Validate snapshot account counts and preserve team profile files, with hashes and account roles compared privately.

## Task 2 — Mac lifecycle and package

Files: scripts/lan-macos/common.sh, setup.command, start.command, stop.command, backup.command, README.md, scripts/lan-macos/package.py.

- [x] Create native Homebrew prerequisite check/install, separate MySQL/Redis initialization, import-once marker, startup/health/LAN address output and exact-process stop logic.
- [x] Generate fresh local secrets, relative paths, no build-tool dependencies or hardcoded Windows/server paths. Back up local work and media without deleting or restoring over live data.
- [x] Freeze source at verified runtime commit, cross-build darwin arm64/amd64 with the current verified embedded frontend, and package the snapshot outside Git.

## Task 3 — Acceptance and delivery

- [x] Run script/policy tests, Bash syntax checks, actual isolated MySQL/Redis bootstrap and Go runtime acceptance on a separate test port; compare imported account hashes, account roles, zero copied sessions/jobs, authentication enforcement and frontend identity.
- [x] Check archive permissions, hashes, secret exclusions and required files; commit only scripts and this evidence document on v88.
- [x] Prepare the deliverable archive with first-install/start instructions and explicit target-Mac acceptance limitation. No remote code push/deployment is included in this packaging request.

## Verified evidence (2026-11-26)

- Private snapshot: 26 online accounts, 25 active. Imported password hashes and owner/active flags match the read-only online aggregate. Nine API configuration files contain 32 encrypted credentials; all decrypt with the copied encryption material. No provider calls were made.
- Isolated MySQL 8.4 and Redis acceptance: Windows candidate on 127.0.0.1:14001 served the embedded frontend `/assets/user-BdLFT_sC.js`; unauthenticated config and wrong password returned 401, a temporary test identity could log in and access its session/config with 200. The test identity was removed. No online sessions or automatic jobs were copied.
- Six packaging policy/architecture/extraction tests and five Bash syntax checks passed. Chinese fallback-font test passed. Actual Linux execution of the disk-space regression test passed for an existing and a missing path.
- Original source failed Darwin compilation because disk-space protection was implemented only in `_linux.go` and `_windows.go`. Rename the Unix implementation and include Darwin in its build constraint; full arm64 and amd64 application builds then succeeded. This preserves the low-space guard.
- Final binaries incorporate the latest maintained upload-name change (78e25625) as well as this Darwin fix. All 2,118 committed backend files were verified against Git blobs for source commit `441b8efe894203194d3f5af12caed83a6ec24430`, and the embedded frontend index hash matched. A fresh isolated login/config/runtime-identity test passed after the final build. Current Windows 4001 and online services remain untouched.
- `YZCM-LAN-Mac-20261126.zip`: 371,051,678 bytes; SHA-256 `590dcbdc25aa06a05f037a6c136c61eb6d8fec65098f0027ac90065b78152233`. ZIP integrity, 30 file hashes, native M4/Intel binary headers, Unix file permissions, allowlisted SQL/config export and shutdown-guard SQL all passed verification. No test account, runtime sessions or test database credentials are included.
- First execution of the Homebrew lifecycle scripts and application on an actual M4 remains unverified because the target Mac is not connected. Cross-compilation and isolated Windows/Linux execution do not substitute for that check.
- Detailed private evidence and the deliverable stay in the ignored `_local-archive-20261028/lan-package-20261126/` folder, never in Git.
