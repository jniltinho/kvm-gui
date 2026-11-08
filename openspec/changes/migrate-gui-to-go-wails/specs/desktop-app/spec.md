# Spec Delta

## Purpose

The `kvm-gui` desktop application as a product: one binary with GUI, CLI and MCP server, its web-based UI, build and packaging, compatibility with existing data, and the rules that keep the repository Go-only.

## ADDED Requirements

### Requirement: One binary
The system SHALL ship one executable, `kvm-gui`, whose command line is one command tree: without arguments or with `gui` it opens the desktop GUI, the CLI is its subcommands, and `mcp` runs the MCP server, with the same subcommand names, flags and output meaning `macos-kvm-gui` had and `--help` on every command.

#### Scenario: Same CLI
- **WHEN** a script that ran `macos-kvm-gui vm list` runs `kvm-gui vm list`
- **THEN** it prints the same output and exit status

#### Scenario: GUI by default
- **WHEN** `kvm-gui` runs with no arguments in a desktop session
- **THEN** the Manager window opens

### Requirement: Embedded user interface
The desktop GUI SHALL be a web UI embedded in the binary and rendered by the system webview. It SHALL load no page, script, style, font or image from the network or from outside the binary.

#### Scenario: Offline start
- **WHEN** the GUI starts on a host with no network
- **THEN** every screen renders completely

#### Scenario: Navigation attempt
- **WHEN** content in the UI tries to navigate the webview to an external URL
- **THEN** the navigation is refused and the URL opens only in the user's browser after the user clicks it

### Requirement: Local trust boundary
Streams between the GUI backend and the web UI (screen frames, input, logs) SHALL bind only to loopback and SHALL accept a connection only from the app's own page origin, refusing a missing, `null` or other origin. The backend SHALL never listen on a non-loopback interface.

#### Scenario: Another local process
- **WHEN** another process on the host connects to the screen stream with a forged or missing origin
- **THEN** the connection is refused and no frame or input is exchanged

### Requirement: Stream authentication
Each stream connection SHALL authenticate within 2 seconds with a secret of at least 128 bits, created for each GUI launch, handed to the page only through the app's internal bridge, never in a URL, log or file. Each connection SHALL be bound to one VM run; it SHALL be closed when that run ends, and oversized messages SHALL close it.

#### Scenario: Wrong or late token
- **WHEN** a client connects with a wrong token, or sends none within 2 seconds
- **THEN** the connection is closed and no frame or input is exchanged

#### Scenario: Stale viewer after a restart
- **WHEN** a VM restarts while an old viewer connection is still open
- **THEN** the old connection is closed and its input never reaches the new run

#### Scenario: Viewer disconnects with a key down
- **WHEN** the viewer's connection drops while Shift and the left button are held
- **THEN** the guest receives the Shift and button releases

### Requirement: Single instance
At most one GUI instance per user SHALL run. Starting a second one SHALL bring the existing Manager window to the front and exit.

#### Scenario: Second launch
- **WHEN** the GUI is open and the user launches it again from the menu
- **THEN** the open Manager window is raised and no second window opens

### Requirement: Newer database schemas
The application SHALL refuse to open a database whose schema is newer than it knows, changing nothing and naming the version it found.

#### Scenario: Newer database
- **WHEN** `kvm-gui` opens a database with a schema newer than it supports
- **THEN** it exits with a message naming that schema version and the file is unchanged

### Requirement: Release artifacts
Each release SHALL publish, for Linux amd64 only: an AppImage (`kvm-gui-<version>-x86_64.AppImage`), a plain binary (`kvm-gui-<version>-linux-amd64`), a `.deb` (`kvm-gui_<version>_amd64.deb`), an `.rpm` (`kvm-gui-<version>-1.x86_64.rpm`) and a `SHA256SUMS` file covering all four. Invoked through a link named `macos-kvm-gui`, every artifact's binary SHALL behave as `kvm-gui`.

#### Scenario: Release assets
- **WHEN** a release is published
- **THEN** its assets are exactly the AppImage, the plain binary, the `.deb`, the `.rpm` and `SHA256SUMS`, and the sums match the files

#### Scenario: Old command name
- **WHEN** a user links `~/.local/bin/macos-kvm-gui` to the plain binary and runs `macos-kvm-gui mcp`
- **THEN** the MCP server starts as with `kvm-gui mcp`

### Requirement: Packages
The `.deb` and `.rpm` packages SHALL be named `kvm-gui`, SHALL declare the runtime libraries and VM tools they need as dependencies, SHALL replace an installed `macos-kvm-gui` package on upgrade, and SHALL install the binary, a `macos-kvm-gui` command link, the desktop entry, the icon, the license and the third-party notices.

#### Scenario: apt upgrade path
- **WHEN** `macos-kvm-gui` is installed and the user installs the `kvm-gui` `.deb` with apt
- **THEN** `macos-kvm-gui` is removed, `kvm-gui` is installed, both commands work, and the VMs are still listed

#### Scenario: dnf on Rocky Linux
- **WHEN** the user installs the `kvm-gui` `.rpm` with dnf on a clean Rocky Linux 10
- **THEN** its dependencies resolve from the distribution's repositories (and EPEL only if the documentation says so) and the menu shows kvm-gui

### Requirement: Supported distributions
The `.deb` SHALL install on Ubuntu 24.04, Ubuntu 26.04 and Debian 13, and the `.rpm` on Rocky Linux 10. The AppImage and the plain binary SHALL run on the same four distributions once the GTK 4 and WebKitGTK 6.0 runtime libraries are installed; the documentation SHALL list, for each distribution, the one command that installs them and the VM tools (QEMU, OVMF, `swtpm`).

#### Scenario: Clean distribution check
- **WHEN** the `.deb` is installed with apt in a clean Ubuntu 24.04, Ubuntu 26.04 or Debian 13 container, the `.rpm` with dnf in a clean Rocky Linux 10 container, or the documented libraries are installed and the AppImage and plain binary are run
- **THEN** each runs `--self-test`, renders the Manager window under a virtual display and exits 0

### Requirement: Local database
The VMs, snapshots and preferences SHALL be kept in one local SQLite database file, `kvm-gui.db` in the app's data folder, opened in-process with no database server, no network access and no SQLite system library needed at runtime.

#### Scenario: Minimal host
- **WHEN** `kvm-gui` runs on a host without any SQLite package installed
- **THEN** it opens, migrates and writes its database normally

### Requirement: Docker-only build
The application, its release artifacts and its checks SHALL build inside Docker from one Ubuntu 24.04 based image; the host SHALL need only Docker and make. One build SHALL produce every release artifact.

#### Scenario: Build
- **WHEN** a developer runs the release build target on a host with only Docker and make
- **THEN** `dist/` holds the AppImage, the plain binary, the `.deb`, the `.rpm` and `SHA256SUMS`, and nothing was installed on the host

### Requirement: Licenses with the artifacts
Every release artifact SHALL carry the project's license and the third-party notices: the binary SHALL print them with `kvm-gui licenses`, and the AppImage, the `.deb` and the `.rpm` SHALL also contain them as files.

#### Scenario: Downloaded AppImage
- **WHEN** a user extracts the released AppImage
- **THEN** it contains `LICENSE` and `THIRD_PARTY_NOTICES.md`, matching `kvm-gui licenses`

### Requirement: Release checks
A release SHALL be published only from a commit whose CI run passed: Go checks and race tests, frontend checks, OpenSpec validation, the embedding test under a virtual display, and the clean-distribution checks for Ubuntu 24.04, Ubuntu 26.04, Debian 13 and Rocky Linux 10. A test that needs a tool SHALL fail in CI when the tool is missing, not skip.

#### Scenario: Red commit
- **WHEN** a release is started for a commit whose clean-distribution check for Rocky Linux 10 failed
- **THEN** nothing is published and the run names the failed check

### Requirement: Themes
Every window and dialog SHALL support the skins Light, Dark, Windows XP, macOS, macOS Dark and Mac OS Retro, plus System, which follows the desktop's light or dark preference live. The choice SHALL apply at once without a restart, and `KVM_GUI_THEME` (or `MKG_THEME`) SHALL override the saved choice.

#### Scenario: System theme
- **WHEN** the theme is System and the desktop switches from light to dark
- **THEN** every open window turns dark without a restart

#### Scenario: Windows XP skin
- **WHEN** the user picks Windows XP in File › Preferences › Interface
- **THEN** every open window and dialog uses that skin at once, and the next start keeps it

### Requirement: Complete skins
Every skin SHALL define the same set of color and shape tokens, so no screen falls back to another skin's colors or shows an undefined color in any skin.

#### Scenario: New token
- **WHEN** a developer adds a token to the Light skin only
- **THEN** the repository's checks fail naming the skins that lack it

### Requirement: HiDPI layout
The UI SHALL render without clipping, overlap, horizontal page scroll or blurry icons at 1920x1080, 2560x1440 and 3840x2160, at device pixel ratios 1, 1.25, 1.5 and 2.

#### Scenario: 4K at 2x
- **WHEN** the Manager window is maximized on a 3840x2160 monitor at scale 2
- **THEN** text and icons are sharp and every toolbar button, list and Details section is fully visible

### Requirement: Accessible controls
Every interactive control SHALL expose a role and an accessible name to the desktop accessibility tree and SHALL be reachable with the keyboard, so that the main workflows can be driven by an accessibility-tree tool without screen coordinates.

#### Scenario: Agent-driven New VM
- **WHEN** an accessibility-tree client lists the Manager window
- **THEN** it finds the New, Settings, Start and Discard toolbar buttons and the VM list items by name and can activate them

### Requirement: Go-only repository
The repository SHALL keep the former Rust GUI only as a read-only archive that no build, test, CI workflow or release uses. The only Rust code that is built SHALL be upstream Reims vGPU's QEMU, inside its own packaging build.

#### Scenario: CI run
- **WHEN** a commit is pushed to `main`
- **THEN** CI runs the Go and frontend checks and builds, and no Rust toolchain or Cargo step runs

#### Scenario: Archived app
- **WHEN** a developer looks for the Rust GUI
- **THEN** it is under `archive/rust/reims-gui/` with a notice that it is archived and unsupported
