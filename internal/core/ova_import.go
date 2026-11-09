package core

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// maxDiskBytes bounds an imported disk's virtual size (spec: refused over 16 TiB).
const maxDiskBytes = 16 << 40

// maxSlug bounds the folder name an import derives from the appliance's VM name.
const maxSlug = 64

// ImportPreview is what an import would create (spec "Preview before import").
type ImportPreview struct {
	Config    Config   `json:"config"`
	Folder    string   `json:"folder"`
	Notes     []string `json:"notes"`
	Disks     []string `json:"disks"`     // "disk0.qcow2: 40.0 GiB, SATA"
	NeedBytes int64    `json:"needBytes"` // free space the extraction needs
}

// PlanImport is the dry run of ImportAppliance: pass 1, the mapping, the destination folder and
// the space check. It writes nothing, not even the destination's parent folders.
func PlanImport(src string, o ImportOptions) (ImportPreview, error) {
	s, pv, err := planAppliance(src, o)
	s.Close()
	return pv, err
}

func planAppliance(src string, o ImportOptions) (_ *applianceSource, pv ImportPreview, err error) {
	s, err := openAppliance(src)
	if err != nil {
		return nil, pv, err
	}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	h := SystemHost()
	p, err := planImport(s.Appliance, o, h.MemTotalMiB(), runtime.NumCPU())
	if err != nil {
		return nil, pv, err
	}
	p.Config.Name = freeName(p.Config.Name) // the name it will really get, shown in the preview
	folder := o.Folder
	if folder == "" {
		slug := Slug(p.Config.Name)
		if len(slug) > maxSlug {
			slug = strings.TrimRight(slug[:maxSlug], "-")
		}
		folder = filepath.Join(WorkspaceRoot(), slug) // the workspace is often "~/KVM-VMs"
	}
	if folder, err = (Config{Workspace: folder}).WorkspaceDir(); err != nil { // "~" expanded, absolute only
		return nil, pv, err
	}
	if o.Folder == "" {
		if folder, err = freeFolder(folder); err != nil {
			return nil, pv, err
		}
	} else if _, err := os.Lstat(folder); err == nil {
		return nil, pv, fmt.Errorf("%s already exists: choose another folder", folder)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, pv, err
	}
	p.Config.Workspace = folder
	if err := validatePlan(p.Config); err != nil {
		return nil, pv, fmt.Errorf("the imported VM would not be valid: %w", err)
	}
	if s.Manifest == "" {
		p.Notes = append(p.Notes, "the appliance has no manifest: its files' integrity cannot be checked")
	}
	need, err := s.extractBytes()
	if err != nil {
		return nil, pv, err
	}
	pv = ImportPreview{Config: p.Config, Folder: folder, Notes: append([]string{}, p.Notes...), Disks: []string{}, NeedBytes: need + extractReserve}
	for i, d := range p.Disks {
		pv.Disks = append(pv.Disks, fmt.Sprintf("%s: %.1f GiB, %s (from %s)", d.Path, float64(d.Capacity)/(1<<30), strings.ToUpper(p.Config.Disks[i].Bus), d.Href))
	}
	// The space check runs on the nearest folder that exists: nothing is created before the import.
	parent := filepath.Dir(folder)
	if free, err := freeBytes(existingAncestor(parent)); err == nil && free < pv.NeedBytes {
		return nil, pv, fmt.Errorf("the import needs %.1f GiB free in %s to extract the appliance, and %.1f GiB are free",
			float64(pv.NeedBytes)/(1<<30), parent, float64(free)/(1<<30))
	}
	return s, pv, nil
}

// existingAncestor is dir or its nearest parent that exists.
func existingAncestor(dir string) string {
	for {
		if _, err := os.Stat(dir); err == nil || dir == filepath.Dir(dir) {
			return dir
		}
		dir = filepath.Dir(dir)
	}
}

// ownedDir is a folder an import created, with its identity: a folder put at that path later by
// someone else is not removed.
type ownedDir struct {
	path string
	info os.FileInfo
}

// mkdirsOwned creates dir and its missing parents one by one and returns the folders this call
// created, deepest first; a folder someone else created meanwhile is not one of them.
func mkdirsOwned(dir string) ([]ownedDir, error) {
	var missing []string
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(d); err == nil || d == filepath.Dir(d) {
			break
		}
		missing = append(missing, d)
	}
	var created []ownedDir
	for i := len(missing) - 1; i >= 0; i-- {
		err := os.Mkdir(missing[i], 0o755)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		var st os.FileInfo
		if err == nil {
			st, err = os.Lstat(missing[i])
		}
		if err != nil {
			removeOwned(created)
			return nil, err
		}
		created = append([]ownedDir{{missing[i], st}}, created...)
	}
	return created, nil
}

// removeOwned removes the folders mkdirsOwned created, deepest first, each only while empty: rmdir
// never removes a file, even one put in a folder's place meanwhile.
func removeOwned(dirs []ownedDir) {
	for _, d := range dirs {
		if st, err := os.Lstat(d.path); err == nil && os.SameFile(st, d.info) {
			unix.Rmdir(d.path)
		}
	}
}

// freeName is name, or the first free "name (imported)", "name (imported 2)"... as
// createImported names a copy, so the preview shows the name the VM gets.
func freeName(name string) string {
	vms, err := ListVMs()
	if err != nil {
		return name
	}
	used := func(n string) bool { // names are unique without regard to case, as in the VM list
		for _, e := range vms {
			if strings.EqualFold(e.Config.Name, n) {
				return true
			}
		}
		return false
	}
	for i := 0; i < 100; i++ {
		n := name
		switch {
		case i == 1:
			n = name + " (imported)"
		case i > 1:
			n = fmt.Sprintf("%s (imported %d)", name, i)
		}
		if !used(n) {
			return n
		}
	}
	return name
}

// freeFolder is dir, or dir-2, dir-3... the first that does not exist; another error than "does
// not exist" (a name too long, no permission) is returned.
func freeFolder(dir string) (string, error) {
	for i := 1; i <= 1000; i++ {
		d := dir
		if i > 1 {
			d = fmt.Sprintf("%s-%d", dir, i)
		}
		_, err := os.Lstat(d)
		if errors.Is(err, os.ErrNotExist) {
			return d, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("no free folder name next to %s", dir)
}

// ImportAppliance imports an .ova or .ovf as a new Linux, Windows or Other VM (design D6-D8):
// files extracted into a private payload folder inside a staging folder (0700) beside the
// destination, their digests checked, each disk's header checked, then inspected and converted
// to qcow2 (0600) by qemu-img with its declared format. Only the converted disks are published,
// by a rename that never replaces anything, and the VM is registered last. Any failure or
// cancellation removes what it wrote, parent folders it created included. progress gets bytes
// done of the total.
func ImportAppliance(ctx context.Context, src string, o ImportOptions, progress func(done, total int64)) (VMEntry, error) {
	s, pv, err := planAppliance(src, o)
	if err != nil {
		return VMEntry{}, err
	}
	defer s.Close()
	p, err := planImport(s.Appliance, o, SystemHost().MemTotalMiB(), runtime.NumCPU())
	if err != nil {
		return VMEntry{}, err
	}
	p.Config = pv.Config
	parent := filepath.Dir(pv.Folder)
	created, err := mkdirsOwned(parent) // made by this import: removed again on failure if still empty
	if err != nil {
		return VMEntry{}, err
	}
	owned := ""
	defer func() {
		if owned != "" {
			os.RemoveAll(owned)
		}
		removeOwned(created)
	}()
	stage, err := os.MkdirTemp(parent, ".import-") // 0700: the guest's disks are the user's only
	if err != nil {
		return VMEntry{}, err
	}
	owned = stage // removed on any failure until the VM is registered
	// The appliance's own files never become the VM's: a converted disk could otherwise collide
	// with a referenced file of the same name, and an OVMF_VARS.fd in it would be published.
	payload := filepath.Join(stage, ".payload")
	if err := os.Mkdir(payload, 0o700); err != nil {
		return VMEntry{}, err
	}
	need, err := s.extractBytes()
	if err != nil {
		return VMEntry{}, err
	}
	total := need // extraction, then each disk's conversion
	for _, d := range p.Disks {
		total += s.Sizes[d.Href]
	}
	var done int64
	report := func(n int64) {
		done += n
		if progress != nil {
			progress(min(done, total), total)
		}
	}
	sums, err := s.extract(ctx, payload, report)
	if err != nil {
		return VMEntry{}, err
	}
	if err := s.checkManifest(payload, sums); err != nil {
		return VMEntry{}, err
	}
	for _, d := range p.Disks {
		in := filepath.Join(payload, d.Href)
		size, base := s.Sizes[d.Href], done
		if err := convertImported(ctx, in, d, filepath.Join(stage, d.Path), func(f float64) {
			if progress != nil {
				progress(min(base+int64(f*float64(size)), total), total)
			}
		}); err != nil {
			return VMEntry{}, fmt.Errorf("%s: %w", d.Href, err)
		}
		report(size)
		if err := os.Remove(in); err != nil {
			return VMEntry{}, err
		}
	}
	// The rest (VirtualBox's NVRAM, a certificate, the manifest) was only checked.
	if err := os.RemoveAll(payload); err != nil {
		return VMEntry{}, err
	}
	if err := ctx.Err(); err != nil {
		return VMEntry{}, err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, pv.Folder, unix.RENAME_NOREPLACE); err != nil {
		return VMEntry{}, fmt.Errorf("moving the imported VM into %s: %w", pv.Folder, err)
	}
	owned = pv.Folder
	e, err := createImported(p.Config)
	if err != nil {
		return VMEntry{}, err
	}
	owned, created = "", nil
	return e, nil
}

// extract runs pass 2 (D6): every referenced file and the manifest into dir, hashing each; the
// descriptor's digests come from the copy pass 1 read. An .ovf's files are opened without
// following links and must be the very files pass 1 saw.
func (s *applianceSource) extract(ctx context.Context, dir string, report func(int64)) (map[string][2]string, error) {
	sums := map[string][2]string{}
	want := map[string]bool{}
	for _, f := range s.Appliance.Files {
		want[f.Href] = true
	}
	if s.Manifest != "" {
		want[s.Manifest] = true
	}
	h1, h2 := sha1.Sum(s.Descriptor), sha256.Sum256(s.Descriptor)
	sums[s.DescName] = [2]string{hex.EncodeToString(h1[:]), hex.EncodeToString(h2[:])}
	copyOne := func(name string, r io.Reader, size int64) error {
		if err := memberName(name); err != nil {
			return err
		}
		out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		a, b := sha1.New(), sha256.New()
		n, err := io.Copy(io.MultiWriter(out, a, b), &ctxReader{ctx: ctx, r: io.LimitReader(r, size+1), report: report})
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err == nil && n != size {
			err = fmt.Errorf("%s ends after %d of its %d bytes", name, n, size)
		}
		if err != nil {
			return err
		}
		sums[name] = [2]string{hex.EncodeToString(a.Sum(nil)), hex.EncodeToString(b.Sum(nil))}
		return nil
	}
	if !s.OVA {
		for name := range want {
			f, _, err := openPlainAt(s.Dir, name, s.Infos[name])
			if err != nil {
				return nil, err
			}
			err = copyOne(name, f, s.Sizes[name])
			f.Close()
			if err != nil {
				return nil, err
			}
		}
		return sums, nil
	}
	f, _, err := openPlainAt(s.Dir, filepath.Base(s.Path), s.Infos[""])
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if !want[h.Name] {
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size != s.Sizes[h.Name] {
			return nil, fmt.Errorf("%s changed since the appliance was checked", h.Name)
		}
		if err := copyOne(h.Name, tr, h.Size); err != nil {
			return nil, err
		}
		delete(want, h.Name)
	}
	if len(want) > 0 {
		return nil, errors.New("the appliance changed since it was checked")
	}
	return sums, nil
}

var manifestLine = regexp.MustCompile(`^(SHA1|SHA256)\s*\(([^)]+)\)\s*=\s*([0-9a-fA-F]+)$`)

// checkManifest verifies the manifest (D6): SHA1 or SHA256 lines, one per file, covering the
// descriptor and every referenced file, each matching. It is read through a bounded reader.
func (s *applianceSource) checkManifest(dir string, sums map[string][2]string) error {
	if s.Manifest == "" {
		return nil
	}
	f, err := os.Open(filepath.Join(dir, s.Manifest))
	if err != nil {
		return err
	}
	defer f.Close()
	seen := map[string]bool{}
	sc := bufio.NewScanner(io.LimitReader(f, maxSmallFile))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		m := manifestLine.FindStringSubmatch(line)
		if m == nil {
			return fmt.Errorf("the manifest has a line kvm-gui cannot read: %q", line)
		}
		name := strings.TrimSpace(m[2])
		if seen[name] {
			return fmt.Errorf("the manifest lists %s twice", name)
		}
		seen[name] = true
		got, ok := sums[name]
		if !ok {
			return fmt.Errorf("the manifest lists %s, which the appliance does not have", name)
		}
		have := got[1]
		if m[1] == "SHA1" {
			have = got[0]
		}
		if !strings.EqualFold(have, m[3]) {
			return fmt.Errorf("%s does not match its digest in the manifest: the appliance is damaged or was changed", name)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("the manifest cannot be read: %w", err)
	}
	for _, name := range append([]string{s.DescName}, hrefs(s.Appliance.Files)...) {
		if !seen[name] {
			return fmt.Errorf("the manifest does not cover %s", name)
		}
	}
	return nil
}

func hrefs(files []ovfFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Href
	}
	return out
}

// ctxReader stops a copy when ctx ends and reports what it read.
type ctxReader struct {
	ctx    context.Context
	r      io.Reader
	report func(int64)
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	if n > 0 && c.report != nil {
		c.report(int64(n))
	}
	return n, err
}

// --- disks ---------------------------------------------------------------------------------

var (
	vmdkCreateType = regexp.MustCompile(`(?m)^[ \t]*createType[ \t]*=[ \t]*"([^"]*)"`)
	vmdkExtent     = regexp.MustCompile(`(?m)^[ \t]*(RW|RDONLY|NOACCESS)[ \t]+\d+[ \t]+(\S+)`)
	vmdkExtentAny  = regexp.MustCompile(`(?m)^[ \t]*(RW|RDONLY|NOACCESS)[ \t]`)
	vmdkParent     = regexp.MustCompile(`(?m)^[ \t]*parent(CID[ \t]*=[ \t]*(\S+)|FileNameHint)`)
)

// checkDiskHeader reads an extracted disk's header itself before qemu-img opens it (D7): qemu-img
// opens a VMDK's extents and parent and a qcow2's backing file while reading metadata.
func checkDiskHeader(path, format string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	hdr := make([]byte, 512)
	n, _ := io.ReadFull(f, hdr)
	hdr = hdr[:n]
	switch format {
	case "raw":
		return nil // raw data is never interpreted: every qemu-img call says -f raw
	case "qcow2":
		if n < 104 || string(hdr[:4]) != "QFI\xfb" {
			return errors.New("it is declared qcow2 but is not one")
		}
		if binary.BigEndian.Uint64(hdr[8:16]) != 0 {
			return errors.New("it has a backing file: refusing an image that refers to another file")
		}
		if v := binary.BigEndian.Uint32(hdr[4:8]); v >= 3 && binary.BigEndian.Uint64(hdr[72:80])&(1<<2) != 0 {
			return errors.New("it keeps its data in an external file: refusing it")
		}
		return nil
	case "vmdk":
		if n < 512 || string(hdr[:4]) != "KDMV" {
			return errors.New("it is not a single-file VMDK (sparse or streamOptimized): VMDKs whose data is in other files are refused")
		}
		// A zero capacity makes QEMU read the embedded descriptor as a descriptor file, extents and
		// all (block/vmdk.c): only a real sparse extent is accepted.
		if binary.LittleEndian.Uint64(hdr[12:20]) == 0 {
			return errors.New("its header declares no capacity: refusing it")
		}
		descOff, descSize := binary.LittleEndian.Uint64(hdr[28:36]), binary.LittleEndian.Uint64(hdr[36:44])
		if descOff == 0 {
			return nil // no embedded descriptor: one extent, this file, no parent
		}
		if descSize == 0 || descSize > 128 || descOff > 1<<40 { // sectors: 64 KiB is far more than any descriptor needs
			return errors.New("its embedded descriptor has an invalid size")
		}
		desc := make([]byte, descSize*512)
		if _, err := f.ReadAt(desc, int64(descOff)*512); err != nil {
			return fmt.Errorf("reading its embedded descriptor: %w", err)
		}
		text := string(bytes.TrimRight(desc, "\x00"))
		ct := vmdkCreateType.FindAllStringSubmatch(text, -1)
		if len(ct) != 1 || (ct[0][1] != "monolithicSparse" && ct[0][1] != "streamOptimized") {
			return errors.New("it is not a single-file VMDK (sparse or streamOptimized): refusing it")
		}
		ext := vmdkExtent.FindAllStringSubmatch(text, -1)
		if n := len(vmdkExtentAny.FindAllString(text, -1)); n != 1 || len(ext) != 1 || ext[0][2] != "SPARSE" {
			return fmt.Errorf("its descriptor lists %d extents: only a single sparse extent is accepted", n)
		}
		for _, m := range vmdkParent.FindAllStringSubmatch(text, -1) {
			if strings.HasPrefix(m[1], "FileNameHint") || !strings.EqualFold(strings.Trim(m[2], `"`), "ffffffff") {
				return errors.New("it is a delta of another disk (it has a parent): refusing it")
			}
		}
		return nil
	}
	return fmt.Errorf("format %q is not supported", format)
}

// limited is qemu-img args under prlimit (D7): an address space bound, and for quick commands a
// CPU-time bound (seconds, 0: none). Without prlimit an untrusted image is not processed.
func limited(asBytes int64, seconds int, args ...string) (string, []string, error) {
	p, err := exec.LookPath("prlimit") // util-linux: on every supported distribution
	if err != nil {
		return "", nil, errors.New("prlimit (util-linux) is missing: kvm-gui does not process an appliance's disks without resource limits")
	}
	pre := []string{"--as=" + strconv.FormatInt(asBytes, 10)}
	if seconds > 0 {
		pre = append(pre, "--cpu="+strconv.Itoa(seconds))
	}
	return p, append(append(pre, "--", "qemu-img"), args...), nil
}

// qemuImgLimited runs a quick qemu-img command under limited, with a wall-clock timeout of twice
// its CPU time (0: none).
func qemuImgLimited(ctx context.Context, asBytes int64, seconds int, args ...string) ([]byte, error) {
	name, full, err := limited(asBytes, seconds, args...)
	if err != nil {
		return nil, err
	}
	if seconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Duration(seconds)*time.Second)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, full...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return out, fmt.Errorf("qemu-img %s: %s", args[0], msg)
		}
		return out, fmt.Errorf("qemu-img %s: %w", args[0], err)
	}
	return out, nil
}

// imgInfo is what convertImported reads from `qemu-img info`.
type imgInfo struct {
	VirtualSize uint64 `json:"virtual-size"`
	Backing     string `json:"backing-filename"`
	Spec        struct {
		Type string `json:"type"`
		Data struct {
			DataFile   string `json:"data-file"`
			CreateType string `json:"create-type"`
			Extents    []struct {
				Filename string `json:"filename"`
			} `json:"extents"`
		} `json:"data"`
	} `json:"format-specific"`
}

// qemu-img -p prints its progress as "    (12.34/100%)".
var convertPct = regexp.MustCompile(`\(([0-9.]+)/100%\)`)

// convertLimited runs qemu-img convert -p under limited, reporting the fraction done.
func convertLimited(ctx context.Context, progress func(float64), args ...string) error {
	name, full, err := limited(4<<30, 0, append([]string{"convert", "-p"}, args...)...)
	if err != nil {
		return err
	}
	return runStream(ctx, "", func(line string) {
		if m := convertPct.FindStringSubmatch(line); m != nil && progress != nil {
			if f, err := strconv.ParseFloat(m[1], 64); err == nil {
				progress(f / 100)
			}
		}
	}, name, full...)
}

// convertImported checks an extracted disk (D7) and converts it to qcow2 at out, 0600.
func convertImported(ctx context.Context, in string, d plannedDisk, out string, progress func(float64)) error {
	if err := checkDiskHeader(in, d.Format); err != nil {
		return err
	}
	b, err := qemuImgLimited(ctx, 2<<30, 120, "info", "-f", d.Format, "--output=json", in)
	if err != nil {
		return err
	}
	var info imgInfo
	if err := json.Unmarshal(b, &info); err != nil {
		return fmt.Errorf("reading qemu-img info: %w", err)
	}
	if info.Backing != "" || info.Spec.Data.DataFile != "" {
		return errors.New("it refers to another file: refusing it")
	}
	if d.Format == "vmdk" {
		if ct := info.Spec.Data.CreateType; ct != "" && ct != "monolithicSparse" && ct != "streamOptimized" {
			return fmt.Errorf("its VMDK type %s spans several files: refusing it", ct)
		}
		abs, _ := filepath.Abs(in)
		for _, x := range info.Spec.Data.Extents {
			if x.Filename != in && x.Filename != abs {
				return errors.New("it has an extent in another file: refusing it")
			}
		}
	}
	if info.VirtualSize > maxDiskBytes {
		return fmt.Errorf("its size %.1f TiB is over the 16 TiB kvm-gui imports", float64(info.VirtualSize)/(1<<40))
	}
	// The descriptor's capacity is in its unit (often GiB): the real size may only round up to it.
	// Disks are whole 512-byte sectors, so the declared size counts rounded up to one.
	declared := (d.Capacity + 511) &^ 511
	if d.Capacity != 0 && (info.VirtualSize > declared || declared-info.VirtualSize >= max(d.Unit, 512)) {
		return fmt.Errorf("it is %d bytes, but the descriptor declares %d", info.VirtualSize, d.Capacity)
	}
	b, err = qemuImgLimited(ctx, 2<<30, 120, "measure", "-f", d.Format, "-O", "qcow2", "--output=json", in)
	if err != nil {
		return err
	}
	var m struct {
		Required int64 `json:"required"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("reading qemu-img measure: %w", err)
	}
	if free, err := freeBytes(filepath.Dir(out)); err == nil && free < m.Required+extractReserve {
		return fmt.Errorf("converting it needs %.1f GiB free, and %.1f GiB are free", float64(m.Required+extractReserve)/(1<<30), float64(free)/(1<<30))
	}
	if err := convertLimited(ctx, progress, "-f", d.Format, "-O", "qcow2", "-o", "compat=1.1", in, out); err != nil {
		os.Remove(out)
		return err
	}
	if progress != nil {
		progress(1)
	}
	if err := os.Chmod(out, 0o600); err != nil {
		os.Remove(out)
		return err
	}
	if err := checkStandaloneDisk(out); err != nil {
		os.Remove(out)
		return err
	}
	return nil
}
