package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registered as "sqlite"
)

// The VMs live in a small SQLite database, <config dir>/macos-kvm-gui/macos-kvm-gui.db, which
// is the source of truth. Files are generated from it: each save writes a snapshot to
// <workspace>/macos-kvm-gui/vm.json, and OpenCore's config.plist is generated at every start.

// VMEntry is one VM. Path is set only for a VM read from a standalone JSON file (--config).
type VMEntry struct {
	ID     string
	Path   string
	Config Config
	// Notice is set by CreateVM when the requested SSH port belonged to another VM and the VM
	// got another one; callers print or log it.
	Notice string
}

// DisplayName is the VM's name, or a name derived from its macOS version.
func (e VMEntry) DisplayName() string {
	if e.Config.Name != "" {
		return e.Config.Name
	}
	return "macOS " + e.Config.Version
}

// DBPath is the database file: ~/.local/share/kvm-gui/kvm-gui.db.
func DBPath() (string, error) { return filepath.Join(DataDir(), "kvm-gui.db"), nil }

const schemaV1 = `
CREATE TABLE IF NOT EXISTS vms (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
	workspace  TEXT NOT NULL,
	version    TEXT NOT NULL,
	ram_mib    INTEGER NOT NULL,
	cores      INTEGER NOT NULL,
	vcpus      INTEGER NOT NULL,
	ssh_port   INTEGER NOT NULL,
	display    TEXT NOT NULL DEFAULT '',
	gpu        TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);`

// schemaVersion is the database's PRAGMA user_version: 1 created the tables, 2 added the
// network adapters, 3 root-patch mode, 4 the graphics adapter, 5 the Reims resolution, 6 the
// guest settings (ext, JSON) and the snapshot tables.
const schemaVersion = 6

const schemaV6Snapshots = `
CREATE TABLE IF NOT EXISTS snapshots (
	id          TEXT PRIMARY KEY,
	vm_id       TEXT NOT NULL,
	parent_id   TEXT NOT NULL DEFAULT '',
	name        TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	created_at  TEXT NOT NULL DEFAULT (datetime('now')),
	state       TEXT NOT NULL DEFAULT '',
	manifest    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS snapshot_ops (id TEXT PRIMARY KEY, done_at TEXT NOT NULL DEFAULT (datetime('now')));`

// migrate brings a database from version v to schemaVersion. Each step is safe to repeat, and
// the version is written last, so an interrupted upgrade runs again on the next start.
func migrate(db *sql.DB, v int) error {
	if v < 1 {
		if _, err := db.Exec(schemaV1); err != nil {
			return err
		}
	}
	if v < 2 {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('vms') WHERE name = 'networks'`).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			// The app and the CLI may upgrade at the same moment: the column the other one
			// just added is fine.
			if _, err := db.Exec(`ALTER TABLE vms ADD COLUMN networks TEXT NOT NULL DEFAULT ''`); err != nil &&
				!strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
	}
	if v < 3 {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('vms') WHERE name = 'root_patch'`).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := db.Exec(`ALTER TABLE vms ADD COLUMN root_patch INTEGER NOT NULL DEFAULT 0`); err != nil &&
				!strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
	}
	if v < 4 {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('vms') WHERE name = 'graphics'`).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := db.Exec(`ALTER TABLE vms ADD COLUMN graphics TEXT NOT NULL DEFAULT ''`); err != nil &&
				!strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
	}
	if v < 5 {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('vms') WHERE name = 'resolution'`).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := db.Exec(`ALTER TABLE vms ADD COLUMN resolution TEXT NOT NULL DEFAULT ''`); err != nil &&
				!strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
	}
	if v < 6 {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('vms') WHERE name = 'ext'`).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := db.Exec(`ALTER TABLE vms ADD COLUMN ext TEXT NOT NULL DEFAULT ''`); err != nil &&
				!strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
		if _, err := db.Exec(schemaV6Snapshots); err != nil {
			return err
		}
	}
	if v < 1 {
		// Imported files are renamed as they are read, so a retry does not read them twice.
		if err := importFiles(db); err != nil {
			return err
		}
	}
	_, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion))
	return err
}

// openDB opens the database, creating and migrating it as needed. WAL and a busy timeout let
// the app and the CLI use it at the same time.
func openDB() (*sql.DB, error) {
	path, err := DBPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := migrateLegacy(path); err != nil {
		return nil, fmt.Errorf("copying macos-kvm-gui's settings and VM list: %w", err)
	}
	// _txlock=immediate: a transaction takes the write lock at BEGIN, so two creates (from
	// either app) cannot pick the same SSH port.
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	// A new database's switch to WAL can return SQLITE_BUSY at once, without the busy timeout,
	// while another process (or connection) makes the same switch: retry for up to about 5 s.
	var v int
	for try := 0; ; try++ {
		err = db.QueryRow(`PRAGMA user_version`).Scan(&v)
		if err == nil || try == 50 || !strings.Contains(err.Error(), "SQLITE_BUSY") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("database %s: %w", path, err)
	}
	if v > schemaVersion {
		db.Close()
		return nil, fmt.Errorf("database %s has schema %d, newer than this kvm-gui knows (%d): use a newer kvm-gui; the file was not changed", path, v, schemaVersion)
	}
	if v < schemaVersion {
		if err := migrate(db, v); err != nil {
			db.Close()
			return nil, fmt.Errorf("upgrading %s: %w", path, err)
		}
	}
	return db, nil
}

const vmColumns = `id, name, workspace, version, ram_mib, cores, vcpus, ssh_port, display, gpu, networks, root_patch, graphics, resolution, ext`

// networksJSON stores the adapters; "" keeps a VM on its legacy single NAT adapter.
func networksJSON(c Config) string {
	if c.Networks == nil {
		return ""
	}
	b, _ := json.Marshal(c.Networks)
	return string(b)
}

// guestExt is the part of Config schema 6 stores as JSON in the ext column: the fields of
// Linux, Windows and Other VMs. "" (every VM from before) is a macOS VM.
type guestExt struct {
	Guest         string         `json:"guest,omitempty"`
	Profile       string         `json:"profile,omitempty"`
	Firmware      string         `json:"firmware,omitempty"`
	SecureBoot    bool           `json:"secure_boot,omitempty"`
	TPM           bool           `json:"tpm,omitempty"`
	Disks         []Disk         `json:"disks,omitempty"`
	Optical       []string       `json:"optical,omitempty"`
	BootOrder     string         `json:"boot_order,omitempty"`
	Serial        string         `json:"serial,omitempty"`
	SharedFolders []SharedFolder `json:"shared_folders,omitempty"`
	USBDevices    []USBDevice    `json:"usb_devices,omitempty"`
	Description   string         `json:"description,omitempty"`
	Backend       string         `json:"backend,omitempty"`
	ReimsCtlName  string         `json:"reims_ctl_name,omitempty"`
	ReimsCtlConf  string         `json:"reims_ctl_config,omitempty"`
	CurrentSnap   string         `json:"current_snapshot,omitempty"`
	Vulkan        bool           `json:"vulkan,omitempty"`
	RenderGPU     string         `json:"render_gpu,omitempty"`
}

func extJSON(c Config) string {
	x := guestExt{c.Guest, c.Profile, c.Firmware, c.SecureBoot, c.TPM, c.Disks, c.Optical, c.BootOrder,
		c.Serial, c.SharedFolders, c.USBDevices, c.Description, c.Backend, c.ReimsCtlName, c.ReimsCtlConfig, c.CurrentSnapshot,
		c.Vulkan, c.RenderGPU}
	if x.Guest == "" && x.Description == "" && x.Backend == "" && x.CurrentSnap == "" {
		return ""
	}
	b, _ := json.Marshal(x)
	return string(b)
}

func applyExt(c *Config, s string) error {
	if s == "" {
		return nil
	}
	var x guestExt
	if err := json.Unmarshal([]byte(s), &x); err != nil {
		return err
	}
	c.Guest, c.Profile, c.Firmware, c.SecureBoot, c.TPM, c.Disks, c.Optical, c.BootOrder = x.Guest, x.Profile, x.Firmware, x.SecureBoot, x.TPM, x.Disks, x.Optical, x.BootOrder
	c.Serial, c.SharedFolders, c.USBDevices, c.Description = x.Serial, x.SharedFolders, x.USBDevices, x.Description
	c.Backend, c.ReimsCtlName, c.ReimsCtlConfig, c.CurrentSnapshot = x.Backend, x.ReimsCtlName, x.ReimsCtlConf, x.CurrentSnap
	c.Vulkan, c.RenderGPU = x.Vulkan, x.RenderGPU
	return nil
}

func scanVM(row interface{ Scan(...any) error }) (VMEntry, error) {
	var e VMEntry
	c := &e.Config
	var nets, ext string
	err := row.Scan(&e.ID, &c.Name, &c.Workspace, &c.Version, &c.RAMMiB, &c.Cores, &c.VCPUs, &c.SSHPort, &c.Display, &c.GPU, &nets, &c.RootPatch, &c.Graphics, &c.Resolution, &ext)
	if err == nil {
		if xerr := applyExt(c, ext); xerr != nil {
			return e, fmt.Errorf("VM %s: unreadable settings: %w", e.ID, xerr)
		}
	}
	if err == nil && nets != "" {
		if jerr := json.Unmarshal([]byte(nets), &c.Networks); jerr != nil {
			return e, fmt.Errorf("VM %s: unreadable network adapters: %w", e.ID, jerr)
		}
	}
	return e, err
}

// ListVMs returns the VMs sorted by name.
func ListVMs() ([]VMEntry, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT ` + vmColumns + ` FROM vms ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var vms []VMEntry
	for rows.Next() {
		e, err := scanVM(rows)
		if err != nil {
			return nil, err
		}
		vms = append(vms, e)
	}
	return vms, rows.Err()
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a name into a file- and id-safe word, for example "Tahoe lab" → "tahoe-lab".
func Slug(name string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if s == "" {
		return "vm"
	}
	return s
}

// DefaultWorkspace is the app's Workspace when none is saved: the parent folder of every VM.
const DefaultWorkspace = "~/KVM-VMs"

// WorkspaceRoot is the folder that holds one subfolder per VM. VMs added before it existed (for
// example an OSX-KVM folder) keep their own folder.
func WorkspaceRoot() string {
	if r := Preference("workspace"); r != "" {
		return r
	}
	return DefaultWorkspace
}

// SetWorkspaceRoot validates and saves the Workspace. Existing VMs are not moved.
func SetWorkspaceRoot(root string) error {
	root = strings.TrimSpace(root)
	dir, err := (Config{Workspace: root}).WorkspaceDir()
	if err != nil {
		return err
	}
	if root = filepath.Clean(root); strings.Contains(root, "..") {
		root = dir // "~/../x" is stored expanded so VMFolder stays absolute
	}
	return SetPreference("workspace", root)
}

// VMFolder is the default folder for a new VM: <Workspace>/<slug of its name>.
func VMFolder(name string) string { return filepath.Join(WorkspaceRoot(), Slug(name)) }

func isUniqueErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// dbtx is a *sql.DB or a *sql.Tx.
type dbtx interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// FirstSSHPort is where the search for a free SSH port starts.
const FirstSSHPort = 2222

// portFree reports whether a port can be bound on 127.0.0.1; tests replace it.
var portFree = func(p int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
	if err != nil {
		return false
	}
	l.Close()
	return true
}

// NextSSHPort is the lowest port from FirstSSHPort up that no VM but exceptID uses and that
// probe (nil: bind on 127.0.0.1) says is free.
func NextSSHPort(exceptID string, probe func(int) bool) (int, error) {
	refreshAllReimsCtl(context.Background())
	db, err := openDB()
	if err != nil {
		return 0, err
	}
	defer db.Close()
	return nextSSHPort(db, exceptID, probe)
}

func nextSSHPort(q dbtx, exceptID string, probe func(int) bool) (int, error) {
	if probe == nil {
		probe = portFree
	}
	rows, err := q.Query(`SELECT ssh_port FROM vms WHERE id <> ?`, exceptID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	used := map[int]bool{}
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			return 0, err
		}
		used[p] = true
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for p := FirstSSHPort; p <= 65535; p++ {
		if !used[p] && probe(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free SSH port from %d to 65535", FirstSSHPort)
}

// SSHPortUsers returns the names of the VMs other than exceptID that use port.
func SSHPortUsers(port int, exceptID string) ([]string, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return sshPortUsers(db, port, exceptID)
}

func sshPortUsers(q dbtx, port int, exceptID string) ([]string, error) {
	rows, err := q.Query(`SELECT name FROM vms WHERE ssh_port = ? AND id <> ? ORDER BY name COLLATE NOCASE`, port, exceptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// SSHPortWarning is the shared-port warning for a VM, or "" when no other VM uses its port.
func SSHPortWarning(port int, exceptID string) string {
	names, err := SSHPortUsers(port, exceptID)
	if err != nil || len(names) == 0 {
		return ""
	}
	return PortWarning(names)
}

// PortWarning words the shared-port warning, the same in both apps.
func PortWarning(names []string) string {
	return "Also used by " + strings.Join(names, ", ") + ": they cannot run at the same time"
}

// CreateVM validates c, stores it as a new VM and returns its entry. SSHPort 0 gets the next
// free port; an explicit port another VM uses is replaced by the next free one, with a Notice.
// The port is picked and the row inserted in one immediate transaction.
func CreateVM(c Config) (VMEntry, error) {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		return VMEntry{}, &FieldError{"name", "give the VM a name"}
	}
	check := c
	if check.SSHPort == 0 {
		check.SSHPort = FirstSSHPort // any valid port: the real one is picked below
	}
	if err := check.Validate(); err != nil {
		return VMEntry{}, err
	}
	refreshAllReimsCtl(context.Background()) // their ports, as reims-ctl has them now
	db, err := openDB()
	if err != nil {
		return VMEntry{}, err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return VMEntry{}, err
	}
	defer tx.Rollback()
	notice := ""
	if c.SSHPort != 0 {
		users, err := sshPortUsers(tx, c.SSHPort, "")
		if err != nil {
			return VMEntry{}, err
		}
		if len(users) > 0 {
			asked := c.SSHPort
			if c.SSHPort, err = nextSSHPort(tx, "", nil); err != nil {
				return VMEntry{}, err
			}
			notice = fmt.Sprintf("SSH port %d belongs to %s; using %d", asked, strings.Join(users, ", "), c.SSHPort)
		}
	} else if c.SSHPort, err = nextSSHPort(tx, "", nil); err != nil {
		return VMEntry{}, err
	}
	e, err := insertVM(tx, c)
	if err != nil {
		return VMEntry{}, err
	}
	if err := tx.Commit(); err != nil {
		return VMEntry{}, err
	}
	writeSnapshot(c)
	e.Notice = notice
	return e, nil
}

func insertVM(db dbtx, c Config) (VMEntry, error) {
	base := Slug(c.Name)
	id := base
	for i := 2; ; i++ {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM vms WHERE id = ?`, id).Scan(&n); err != nil {
			return VMEntry{}, err
		}
		if n == 0 {
			break
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
	_, err := db.Exec(`INSERT INTO vms (`+vmColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, c.Name, c.Workspace, c.Version, c.RAMMiB, c.Cores, c.VCPUs, c.SSHPort, c.Display, c.GPU, networksJSON(c), c.RootPatch, c.Graphics, c.Resolution, extJSON(c))
	if isUniqueErr(err) {
		return VMEntry{}, &FieldError{"name", "a VM with this name already exists"}
	}
	if err != nil {
		return VMEntry{}, err
	}
	return VMEntry{ID: id, Config: c}, nil
}

// SaveVM validates and stores new settings for e, and refreshes its generated snapshot.
func SaveVM(e VMEntry, c Config) error {
	if e.Path != "" { // a standalone --config file
		return SaveConfig(e.Path, c)
	}
	if e.Config.Backend == BackendReimsCtl {
		return saveReimsCtl(e, c)
	}
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		return &FieldError{"name", "give the VM a name"}
	}
	if err := c.Validate(); err != nil {
		return err
	}
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := updateVM(db, e.ID, c); err != nil {
		return err
	}
	writeSnapshot(c)
	return nil
}

func updateVM(db dbtx, id string, c Config) error {
	res, err := db.Exec(`UPDATE vms SET name = ?, workspace = ?, version = ?, ram_mib = ?, cores = ?, vcpus = ?, ssh_port = ?,
		display = ?, gpu = ?, networks = ?, root_patch = ?, graphics = ?, resolution = ?, ext = ?, updated_at = datetime('now') WHERE id = ?`,
		c.Name, c.Workspace, c.Version, c.RAMMiB, c.Cores, c.VCPUs, c.SSHPort, c.Display, c.GPU, networksJSON(c), c.RootPatch, c.Graphics, c.Resolution, extJSON(c), id)
	if isUniqueErr(err) {
		return &FieldError{"name", "a VM with this name already exists"}
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("VM %s no longer exists", id)
	}
	return nil
}

// snapshotFile is the generated, read-only copy of a VM's settings inside its workspace.
const snapshotFile = BootDir + "/vm.json"

// writeSnapshot writes the generated copy when the workspace exists; it never creates one.
func writeSnapshot(c Config) {
	if c.Backend == BackendReimsCtl { // reims-ctl's folder: the app writes nothing there
		return
	}
	dir, err := c.WorkspaceDir()
	if err != nil || !exists(dir) {
		return
	}
	data, _ := json.MarshalIndent(struct {
		Note   string `json:"_note"`
		Config Config `json:"config"`
	}{"Generated from macos-kvm-gui.db; edit the VM in the app or with the CLI, not here.", c}, "", "  ")
	dirMode, fileMode := os.FileMode(0o755), os.FileMode(0o644)
	if !c.MacOS() { // a Linux, Windows or Other VM's folder is private (TPM state, disks)
		dirMode, fileMode = 0o700, 0o600
	}
	if os.MkdirAll(filepath.Join(dir, BootDir), dirMode) == nil {
		writeFileAtomic(filepath.Join(dir, snapshotFile), data, fileMode)
	}
}

// ExportVM returns e's settings as JSON, the format --config reads.
func ExportVM(e VMEntry) ([]byte, error) { return json.MarshalIndent(e.Config, "", "  ") }

// workspaceFiles are the files and folders the app itself creates in a workspace; nothing
// else there is ever deleted.
var workspaceFiles = []string{DiskFile, MediaFile, mediaMarker, stagingDir, BootDir}

// ownedFiles are the files and folders the app created in c's folder: the macOS set, or for a
// Linux, Windows or Other VM its disks inside the folder, UEFI variables, TPM state and serial log.
// Disks elsewhere and ISO files are only used, never moved or deleted.
func ownedFiles(c Config) []string {
	if c.Backend == BackendReimsCtl {
		return nil // reims-ctl's files: `reims-ctl delete` removes them
	}
	if c.MacOS() {
		return append(slices.Clone(workspaceFiles), snapDir)
	}
	files := []string{GuestVarsFile, GuestVarsKind, GuestTPMDir, GuestSerialLog, snapDir}
	for _, d := range c.Disks {
		if !filepath.IsAbs(d.Path) {
			files = append(files, filepath.Clean(d.Path))
		}
	}
	return files
}

// DeleteVM removes the VM. With purge it also deletes, under the workspace lock, the files the
// app created in the workspace, and the workspace folder if it ends up empty. It returns what
// was deleted.
func DeleteVM(e VMEntry, purge bool) ([]string, error) {
	var removed []string
	if purge {
		if _, ok := FindRun(e); ok {
			return nil, errors.New("shut down the VM before deleting its files")
		}
		dir, err := e.Config.WorkspaceDir()
		if err != nil {
			return nil, err
		}
		w := NewWorkspace(dir)
		// The locks a start takes (folder and every image), and the checks again under them.
		unlock, err := (&VM{ID: e.ID}).lockRun(w, e.Config)
		if err != nil {
			return nil, fmt.Errorf("cannot delete its files: %w", err)
		}
		err = func() error {
			defer unlock()
			if err := checkNotShared(e, "delete its files"); err != nil {
				return err
			}
			for _, f := range ownedFilesOf(e) {
				p := w.Path(f)
				if !exists(p) {
					continue
				}
				if err := os.RemoveAll(p); err != nil {
					return err
				}
				removed = append(removed, p)
			}
			if entries, err := os.ReadDir(w.Dir); err == nil && len(entries) == 0 && os.Remove(w.Dir) == nil {
				removed = append(removed, w.Dir)
			}
			return nil
		}()
		if err != nil {
			return removed, err
		}
	}
	if e.Path != "" {
		if err := os.Remove(e.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
		return append(removed, e.Path), nil
	}
	db, err := openDB()
	if err != nil {
		return removed, err
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM vms WHERE id = ?`, e.ID); err != nil {
		return removed, err
	}
	db.Exec(`DELETE FROM snapshots WHERE vm_id = ?`, e.ID) // its tree goes with it
	return append(removed, "VM "+e.ID+" (database)"), nil
}

// SelectedVM returns the remembered VM, or the first one.
func SelectedVM(vms []VMEntry) (VMEntry, bool) {
	if len(vms) == 0 {
		return VMEntry{}, false
	}
	if db, err := openDB(); err == nil {
		defer db.Close()
		var id string
		if db.QueryRow(`SELECT value FROM settings WHERE key = 'selected'`).Scan(&id) == nil {
			for _, e := range vms {
				if e.ID == id {
					return e, true
				}
			}
		}
	}
	return vms[0], true
}

// SelectVM remembers the VM the app and the CLI act on.
func SelectVM(id string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`INSERT INTO settings (key, value) VALUES ('selected', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, id)
	return err
}

// FindVM looks a VM up by id, then by name (case-insensitive): an id always wins, so a VM
// later named like another VM's id cannot be picked instead of it.
func FindVM(vms []VMEntry, key string) (VMEntry, bool) {
	for _, e := range vms {
		if e.ID == key {
			return e, true
		}
	}
	for _, e := range vms {
		if strings.EqualFold(e.DisplayName(), key) {
			return e, true
		}
	}
	return VMEntry{}, false
}

// importFiles moves VMs stored as files by older versions into a new database: the JSON files
// under vms/ and the single-VM config.json. The originals are renamed, not deleted.
func importFiles(db *sql.DB) error {
	dir := legacyConfigDir()
	vmsDir := filepath.Join(dir, "vms")
	files, _ := filepath.Glob(filepath.Join(vmsDir, "*.json"))
	legacy := filepath.Join(dir, "config.json")
	if exists(legacy) {
		files = append(files, legacy)
	}
	for _, f := range files {
		c, err := LoadConfig(f)
		if err != nil {
			return fmt.Errorf("importing %s: %w", f, err)
		}
		if c.Name == "" {
			c.Name = "macOS " + versionDisplay(c.Version)
		}
		if _, err := insertVM(db, c); err != nil {
			var fe *FieldError
			if !errors.As(err, &fe) { // a duplicate name is skipped; anything else stops the import
				return fmt.Errorf("importing %s: %w", f, err)
			}
		} else {
			writeSnapshot(c)
		}
	}
	if sel, err := os.ReadFile(filepath.Join(dir, "selected")); err == nil {
		db.Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES ('selected', ?)`, strings.TrimSpace(string(sel)))
		os.Rename(filepath.Join(dir, "selected"), filepath.Join(dir, "selected.migrated"))
	}
	if exists(vmsDir) {
		os.Rename(vmsDir, vmsDir+".migrated")
	}
	if exists(legacy) {
		os.Rename(legacy, legacy+".migrated")
	}
	return nil
}

func versionDisplay(short string) string {
	for _, v := range Versions {
		if v.Short == short {
			name, _, _ := strings.Cut(v.Name, " (")
			return name
		}
	}
	return short
}

// MoveVM moves the files the app created for e (disk, install media, NVRAM, identity, boot
// files) into dst, which must be new or empty, and points the VM there. Files are renamed, so dst
// must be on the same filesystem; on any error everything is put back. Other files in the old
// folder (for example an OSX-KVM checkout) stay where they are. Both folders are locked.
func MoveVM(e VMEntry, dst string) (VMEntry, error) {
	if e.Path == "" { // use the stored entry, not a copy that may be out of date
		vms, err := ListVMs()
		if err != nil {
			return e, err
		}
		cur, ok := FindVM(vms, e.ID)
		if !ok {
			return e, fmt.Errorf("VM %s no longer exists", e.ID)
		}
		e = cur
	}
	if err := refuseReimsCtl(e.Config, "Move"); err != nil {
		return e, err
	}
	if _, ok := FindRun(e); ok {
		return e, errors.New("shut down the VM before moving it")
	}
	c := e.Config
	c.Workspace = strings.TrimSpace(dst)
	to, err := c.WorkspaceDir()
	if err != nil {
		return e, err
	}
	from, err := e.Config.WorkspaceDir()
	if err != nil {
		return e, err
	}
	if filepath.Clean(from) == to {
		return e, nil
	}
	if rel, err := filepath.Rel(from, to); err == nil && !strings.HasPrefix(rel, "..") {
		return e, errors.New("the new folder cannot be inside the current one")
	}
	src := NewWorkspace(from)
	unlockSrc, err := (&VM{ID: e.ID}).lockRun(src, e.Config)
	if err != nil {
		return e, fmt.Errorf("cannot move it: %w", err)
	}
	defer unlockSrc()
	if err := checkNotShared(e, "move it"); err != nil {
		return e, err
	}
	err = func() error {
		return NewWorkspace(to).withLock(func() error {
			if err := requireEmpty(to); err != nil {
				return err
			}
			if _, err := os.Stat(from); err != nil {
				return fmt.Errorf("cannot read the current folder: %w", err)
			}
			created := !exists(to)
			if err := os.MkdirAll(to, 0o755); err != nil {
				return err
			}
			var moved []string
			undo := func() {
				for _, f := range moved {
					os.Rename(filepath.Join(to, f), src.Path(f))
				}
				if created {
					os.Remove(to)
				}
			}
			for _, f := range ownedFilesOf(e) {
				if _, err := os.Lstat(src.Path(f)); errors.Is(err, os.ErrNotExist) {
					continue
				} else if err != nil {
					undo()
					return err
				}
				if _, err := os.Lstat(filepath.Join(to, f)); !errors.Is(err, os.ErrNotExist) {
					undo()
					return fmt.Errorf("%s already exists in the new folder", f)
				}
				if err := os.Rename(src.Path(f), filepath.Join(to, f)); err != nil {
					undo()
					return fmt.Errorf("moving %s: %w (the new folder must be on the same disk)", f, err)
				}
				moved = append(moved, f)
			}
			if err := SaveVM(e, c); err != nil {
				undo()
				return err
			}
			os.Remove(from) // only when nothing else is left there
			return nil
		})
	}()
	if err != nil {
		return e, err
	}
	e.Config = c
	return e, nil
}

// requireEmpty fails unless dir is missing or an empty, readable folder.
func requireEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot check %s: %w", dir, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s is not empty: choose a new folder", dir)
	}
	return nil
}

// RequireEmpty is requireEmpty for the GUI's checks before creating a VM.
func RequireEmpty(dir string) error { return requireEmpty(dir) }

// CreateVMWithDefaults is `vm create` (and MCP vm_create): a VM with the CPU and RAM recommended
// for this host, a fresh network adapter, sshPort (0: the next free one), its folder (default
// <Workspace>/<name>) prepared with the boot files and an 80 GiB thin disk (unless the folder
// has one), and selected, so it can start. It downloads nothing. A disk warning goes in Notice.
func CreateVMWithDefaults(ctx context.Context, name, version, folder string, sshPort int) (VMEntry, error) {
	c := Recommended(Defaults(), runtime.NumCPU(), SystemHost().MemTotalMiB())
	c.Name, c.Version, c.Workspace, c.SSHPort = name, version, folder, sshPort
	if c.Workspace == "" {
		c.Workspace = VMFolder(c.Name)
	}
	c.Networks = NewNetworks(c.Version)
	e, err := CreateVM(c)
	if err != nil {
		return e, err
	}
	if err := SelectVM(e.ID); err != nil {
		return e, err
	}
	dir, _ := e.Config.WorkspaceDir()
	w := NewWorkspace(dir)
	if err := w.Prepare(e.Config); err != nil {
		return e, err
	}
	if w.HasDisk() {
		return e, nil
	}
	warning, err := w.CreateDisk(ctx, DefaultDiskGiB)
	if warning != "" {
		e.Notice = strings.TrimPrefix(e.Notice+"; disk: "+warning, "; ")
	}
	return e, err
}

// DefaultDiskGiB is a new VM's disk size.
const DefaultDiskGiB = 80

// checkNotShared refuses to move or delete a VM's own files while another VM, or another VM's
// snapshot, uses one of them (B attached A's disk): that VM would lose it.
func checkNotShared(e VMEntry, action string) error {
	dir, err := e.Config.WorkspaceDir()
	if err != nil {
		return err
	}
	var mine []os.FileInfo
	for _, f := range ownedFilesOf(e) {
		if st, err := os.Stat(filepath.Join(dir, f)); err == nil && st.Mode().IsRegular() {
			mine = append(mine, st)
		}
	}
	if len(mine) == 0 {
		return nil
	}
	shared := func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && slices.ContainsFunc(mine, func(m os.FileInfo) bool { return os.SameFile(m, st) })
	}
	vms, err := ListVMs()
	if err != nil {
		return err
	}
	for _, o := range vms {
		if o.ID == e.ID {
			continue
		}
		odir, _ := o.Config.WorkspaceDir()
		for _, st := range diskFiles(o.Config) {
			if slices.ContainsFunc(mine, func(m os.FileInfo) bool { return os.SameFile(m, st) }) {
				return fmt.Errorf("cannot %s: %s uses one of its disks", action, o.DisplayName())
			}
		}
		snaps, _ := listSnapshots(o.ID, true)
		for _, s := range snaps {
			for _, d := range s.Manifest.Disks {
				if shared(diskAbs(odir, d)) {
					return fmt.Errorf("cannot %s: a snapshot of %s uses one of its disks", action, o.DisplayName())
				}
			}
		}
	}
	return nil
}

// ownedFilesOf is ownedFiles plus the VM's own disks that only its snapshots still use (detached
// since): moved and purged with the VM, never left behind; images outside the folder are not.
func ownedFilesOf(e VMEntry) []string {
	files := ownedFiles(e.Config)
	if e.Config.Backend != "" || e.Config.MacOS() || e.ID == "" {
		return files
	}
	snaps, _ := listSnapshots(e.ID, true)
	for _, s := range snaps {
		for _, d := range s.Manifest.Disks {
			if !filepath.IsAbs(d) && filepath.Base(d) == d && !slices.Contains(files, d) {
				files = append(files, d)
			}
		}
	}
	return files
}
