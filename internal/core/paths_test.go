package core

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A schema 5 database becomes schema 6 with every VM a macOS VM; a newer one is refused, unchanged.
func TestSchema6(t *testing.T) {
	isolateXDG(t, t.TempDir())
	path, _ := DBPath()
	os.MkdirAll(filepath.Dir(path), 0o755)
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(db, 0); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO vms (id, name, workspace, version, ram_mib, cores, vcpus, ssh_port) VALUES ('old', 'Old', '/tmp/old', 'sonoma', 8192, 4, 8, 2222)`)
	db.Exec(`PRAGMA user_version = 5`)
	db.Close()
	vms, err := ListVMs()
	if err != nil || len(vms) != 1 || !vms[0].Config.MacOS() || vms[0].Config.Version != "sonoma" {
		t.Fatalf("after migration: %+v %v", vms, err)
	}
	db, _ = sql.Open("sqlite", "file:"+path)
	var v, n int
	db.QueryRow(`PRAGMA user_version`).Scan(&v)
	db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('snapshots', 'snapshot_ops')`).Scan(&n)
	if v != 6 || n != 2 {
		t.Fatalf("version %d, snapshot tables %d", v, n)
	}
	// A Linux VM round-trips its guest settings.
	c := testGuest(t, "ubuntu")
	c.Name, c.Description = "Ubuntu", "a note"
	c.SharedFolders = []SharedFolder{{Path: "/srv/s", Tag: "s"}}
	e, err := CreateVM(c)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := FindVM(mustList(t), e.ID)
	if got.Config.Guest != GuestLinux || got.Config.Profile != "ubuntu" || got.Config.Disks[0].Bus != BusVirtio ||
		got.Config.Description != "a note" || got.Config.SharedFolders[0].Tag != "s" || got.Config.Optical[0] != "/isos/install.iso" {
		t.Fatalf("round trip: %+v", got.Config)
	}
	// Schema 7: refused, and the file is not changed.
	db.Exec(`PRAGMA user_version = 7`)
	db.Close()
	before := fileSum(t, path)
	if _, err := ListVMs(); err == nil || !strings.Contains(err.Error(), "schema 7") {
		t.Fatalf("newer schema: %v", err)
	}
	if fileSum(t, path) != before {
		t.Fatal("the newer database was changed")
	}
}

func mustList(t *testing.T) []VMEntry {
	t.Helper()
	vms, err := ListVMs()
	if err != nil {
		t.Fatal(err)
	}
	return vms
}

func fileSum(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// First start after macos-kvm-gui: the settings and the VM list are copied, the old folder stays
// byte-identical, VMs keep their folders, and new VMs go to ~/KVM-VMs (task 3.2).
func TestMigrateLegacy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		t.Setenv(v, "") // the defaults under HOME
	}
	old := filepath.Join(home, ".config", "macos-kvm-gui")
	os.MkdirAll(old, 0o755)
	db, err := sql.Open("sqlite", "file:"+filepath.Join(old, "macos-kvm-gui.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(db, 0); err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO vms (id, name, workspace, version, ram_mib, cores, vcpus, ssh_port, graphics) VALUES
		('nilton', 'Nilton', '~/macOS-VMs/nilton', 'sequoia', 16384, 2, 4, 2223, 'reims-vgpu'),
		('osx', 'OSX-KVM', '~/OSX-KVM', 'sonoma', 8192, 4, 8, 2222, '')`)
	db.Exec(`INSERT INTO settings (key, value) VALUES ('workspace', '~/macOS-VMs'), ('theme', 'dark')`)
	db.Exec(`PRAGMA user_version = 5`)
	db.Close()
	write(t, filepath.Join(old, "settings.yaml"), "theme: light # mine\nreims_qemu: \"reims/bin/qemu-system-x86_64\" # local\nassets_dir: /abs/assets\n")
	write(t, filepath.Join(old, ".env"), "MKG_THEME=dark\n")
	write(t, filepath.Join(old, "known_hosts"), "[127.0.0.1]:2223 ssh-ed25519 AAAA\n")
	write(t, filepath.Join(home, "macOS-VMs", ".installers", "24H32", "info.json"), "{}")
	sums := map[string]string{}
	filepath.Walk(old, func(p string, i os.FileInfo, _ error) error {
		if !i.IsDir() {
			sums[p] = fileSum(t, p)
		}
		return nil
	})

	// The GUI reads the theme first: that must not hide the old settings.yaml behind a default one.
	if Setting("MKG_THEME") != "light" {
		t.Fatalf("theme from the migrated settings.yaml: %q", Setting("MKG_THEME"))
	}
	vms := mustList(t)
	if len(vms) != 2 {
		t.Fatalf("VMs: %+v", vms)
	}
	for _, e := range vms {
		want := map[string]string{"nilton": "~/macOS-VMs/nilton", "osx": "~/OSX-KVM"}[e.ID]
		if e.Config.Workspace != want || !e.Config.MacOS() {
			t.Errorf("%s: folder %s", e.ID, e.Config.Workspace)
		}
	}
	if !exists(filepath.Join(home, ".local", "share", "kvm-gui", "kvm-gui.db")) {
		t.Fatal("no database in ~/.local/share/kvm-gui")
	}
	newCfg := filepath.Join(home, ".config", "kvm-gui")
	if s := readFile(t, filepath.Join(newCfg, "settings.yaml")); !strings.Contains(s, `reims_qemu: "`+filepath.Join(old, "reims/bin/qemu-system-x86_64")+`" # local`) ||
		!strings.Contains(s, "assets_dir: /abs/assets") || !strings.Contains(s, "theme: light") || !strings.Contains(s, "# local") {
		t.Fatalf("settings.yaml:\n%s", s)
	}
	if readFile(t, filepath.Join(newCfg, ".env")) != "MKG_THEME=dark\n" || !exists(filepath.Join(newCfg, "known_hosts")) {
		t.Fatal(".env or known_hosts not copied")
	}
	if st, _ := os.Stat(filepath.Join(newCfg, "known_hosts")); st.Mode().Perm() != 0o600 {
		t.Fatalf("known_hosts mode %v", st.Mode())
	}
	if WorkspaceRoot() != "~/KVM-VMs" || Preference("legacy_workspace") != "~/macOS-VMs" || Preference("theme") != "dark" {
		t.Fatalf("workspace %q legacy %q", WorkspaceRoot(), Preference("legacy_workspace"))
	}
	if d := legacyInstallersDir(); d != filepath.Join(home, "macOS-VMs", ".installers") {
		t.Fatalf("old installers: %s", d)
	}
	filepath.Walk(old, func(p string, i os.FileInfo, _ error) error {
		if !i.IsDir() && sums[p] != fileSum(t, p) {
			t.Errorf("%s changed", p)
		}
		if !i.IsDir() && sums[p] == "" {
			t.Errorf("%s was added to the old folder", p)
		}
		return nil
	})
	// Nothing is written outside the XDG folders.
	entries, _ := os.ReadDir(home)
	for _, e := range entries {
		if n := e.Name(); n != ".config" && n != ".local" && n != "macOS-VMs" && n != ".cache" {
			t.Errorf("unexpected %s in the home folder", n)
		}
	}
}

// A Workspace the user chose stays; skins are validated (task 3.2).
func TestMigrateKeepsChosenWorkspaceAndThemes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		t.Setenv(v, "")
	}
	old := filepath.Join(home, ".config", "macos-kvm-gui")
	os.MkdirAll(old, 0o755)
	db, _ := sql.Open("sqlite", "file:"+filepath.Join(old, "macos-kvm-gui.db"))
	migrate(db, 0)
	db.Exec(`INSERT INTO settings (key, value) VALUES ('workspace', '/data/vms')`)
	db.Close()
	if WorkspaceRoot() != "/data/vms" || Preference("legacy_workspace") != "/data/vms" {
		t.Fatalf("chosen workspace: %q %q", WorkspaceRoot(), Preference("legacy_workspace"))
	}
	for in, want := range map[string]string{"xp": "xp", "macos-retro": "macos-retro", "dark": "dark", "neon": "system", "": "system"} {
		if ThemeOrSystem(in) != want {
			t.Errorf("ThemeOrSystem(%q) = %q", in, ThemeOrSystem(in))
		}
	}
	// Relative XDG variables fall back to the defaults.
	t.Setenv("XDG_DATA_HOME", "relative/dir")
	if DataDir() != filepath.Join(home, ".local", "share", "kvm-gui") {
		t.Fatalf("relative XDG_DATA_HOME: %s", DataDir())
	}
}
