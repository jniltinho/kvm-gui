package core

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"
)

// kvm-gui keeps its files in the XDG base directories (spec file-layout):
//
//	ConfigDir  $XDG_CONFIG_HOME/kvm-gui   settings.yaml, .env, known_hosts, reims-ctl/
//	DataDir    $XDG_DATA_HOME/kvm-gui     kvm-gui.db, reims-ctl/ (Reims builds), bin/
//	CacheDir   $XDG_CACHE_HOME/kvm-gui    installers/, kexts/, reims-build/
//	StateDir   $XDG_STATE_HOME/kvm-gui    logs/
//
// A relative or empty XDG variable falls back to its default under the home folder.

const appDirName = "kvm-gui"

// legacyAppDir is the folder macos-kvm-gui used under ~/.config.
const legacyAppDir = "macos-kvm-gui"

func xdgBase(env string, fallback ...string) string {
	if v := os.Getenv(env); filepath.IsAbs(v) {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/"
	}
	return filepath.Join(append([]string{home}, fallback...)...)
}

// ConfigDir is ~/.config/kvm-gui.
func ConfigDir() string { return filepath.Join(xdgBase("XDG_CONFIG_HOME", ".config"), appDirName) }

// DataDir is ~/.local/share/kvm-gui.
func DataDir() string { return filepath.Join(xdgBase("XDG_DATA_HOME", ".local", "share"), appDirName) }

// CacheDir is ~/.cache/kvm-gui: everything in it can be downloaded or built again.
func CacheDir() string { return filepath.Join(xdgBase("XDG_CACHE_HOME", ".cache"), appDirName) }

// StateDir is ~/.local/state/kvm-gui.
func StateDir() string {
	return filepath.Join(xdgBase("XDG_STATE_HOME", ".local", "state"), appDirName)
}

// legacyConfigDir is macos-kvm-gui's ~/.config/macos-kvm-gui, read once by the migration.
func legacyConfigDir() string {
	return filepath.Join(xdgBase("XDG_CONFIG_HOME", ".config"), legacyAppDir)
}

// legacyWorkspaceDir is the Workspace macos-kvm-gui kept its shared files in (.installers,
// .kexts, .reims): read as a fallback, never written.
func legacyWorkspaceDir() string {
	ws := Preference("legacy_workspace")
	if ws == "" {
		ws = legacyDefaultWorkspace
	}
	dir, err := (Config{Workspace: ws}).WorkspaceDir()
	if err != nil {
		return ""
	}
	return dir
}

// legacyDefaultWorkspace was macos-kvm-gui's default Workspace.
const legacyDefaultWorkspace = "~/macOS-VMs"

// migrateLegacy runs once, when kvm-gui has no database yet and macos-kvm-gui left one: it copies
// settings.yaml (relative paths made absolute against the old folder), .env and known_hosts to
// ConfigDir, and a consistent copy of the database to DataDir. The old folder is not changed, so
// the previous macos-kvm-gui keeps working on its own copy.
func migrateLegacy(newDB string) error {
	oldDir := legacyConfigDir()
	oldDB := filepath.Join(oldDir, "macos-kvm-gui.db")
	if exists(newDB) || !exists(oldDB) {
		return nil
	}
	if err := os.MkdirAll(DataDir(), 0o755); err != nil {
		return err
	}
	// Two processes may start at once: one migrates, the other waits and finds the copy.
	lock, err := os.OpenFile(filepath.Join(DataDir(), ".migrate.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	if exists(newDB) {
		return nil
	}
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		return err
	}
	for _, f := range []string{"settings.yaml", ".env", "known_hosts"} {
		src := filepath.Join(oldDir, f)
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		if f == "settings.yaml" {
			data = []byte(absoluteSettings(string(data), oldDir))
		}
		mode := os.FileMode(0o644)
		if f == "known_hosts" {
			mode = 0o600
		}
		if dst := filepath.Join(ConfigDir(), f); !exists(dst) {
			if err := writeFileAtomic(dst, data, mode); err != nil {
				return err
			}
		}
	}
	old, err := sql.Open("sqlite", "file:"+oldDB+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer old.Close()
	tmp := newDB + ".migrating"
	os.Remove(tmp)
	defer os.Remove(tmp)
	if _, err := old.Exec(`VACUUM INTO ?`, tmp); err != nil {
		return fmt.Errorf("copying %s: %w", oldDB, err)
	}
	// Finished on the copy, then published by one rename: an interrupted migration leaves no
	// database, so the next start migrates again.
	if err := normalizeMigrated(tmp); err != nil {
		return err
	}
	return os.Rename(tmp, newDB)
}

// normalizeMigrated: the old default Workspace gives way to ~/KVM-VMs for new VMs, one the user
// chose stays, and either way the old one is remembered for its shared files.
func normalizeMigrated(path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	var ws string
	db.QueryRow(`SELECT value FROM settings WHERE key = 'workspace'`).Scan(&ws)
	legacy := ws
	if legacy == "" {
		legacy = legacyDefaultWorkspace
	}
	if _, err := db.Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES ('legacy_workspace', ?)`, legacy); err != nil {
		return err
	}
	if ws == "" || ws == legacyDefaultWorkspace {
		if _, err := db.Exec(`DELETE FROM settings WHERE key = 'workspace'`); err != nil {
			return err
		}
	}
	return db.Close()
}

// ensureMigrated runs migrateLegacy before anything reads or creates kvm-gui's own files.
func ensureMigrated() {
	if path, err := DBPath(); err == nil {
		migrateLegacy(path) // an error shows again when the database opens
	}
}

// absoluteSettings rewrites relative reims_qemu and assets_dir values of settings.yaml as absolute
// paths under base, the folder they were relative to. It edits the parsed YAML, so quoting and
// comments keep their meaning; a file it cannot parse is copied as it is.
func absoluteSettings(text, base string) string {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return text
	}
	m := doc.Content[0]
	changed := false
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if (k.Value == "reims_qemu" || k.Value == "assets_dir") && v.Kind == yaml.ScalarNode &&
			v.Value != "" && !filepath.IsAbs(v.Value) && !strings.HasPrefix(v.Value, "~") {
			v.Value = filepath.Join(base, v.Value)
			changed = true
		}
	}
	if !changed {
		return text
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return text
	}
	return string(out)
}

// firstExisting is the first path that exists, or the first one.
func firstExisting(paths ...string) string {
	for _, p := range paths {
		if p != "" && exists(p) {
			return p
		}
	}
	return paths[0]
}
