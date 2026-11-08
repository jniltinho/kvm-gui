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
)

// maxDiskBytes bounds an imported disk's virtual size (spec: refused over 16 TiB).
const maxDiskBytes = 16 << 40

// ImportPreview is what an import would create (spec "Preview before import").
type ImportPreview struct {
	Config    Config   `json:"config"`
	Folder    string   `json:"folder"`
	Notes     []string `json:"notes"`
	Disks     []string `json:"disks"`     // "disk0.qcow2: 40.0 GiB, SATA"
	NeedBytes int64    `json:"needBytes"` // free space the extraction needs
}

// PlanImport is the dry run of ImportAppliance: pass 1, the mapping, the destination folder and
// the space check. It writes nothing.
func PlanImport(src string, o ImportOptions) (ImportPreview, error) {
	_, pv, err := planAppliance(src, o)
	return pv, err
}

func planAppliance(src string, o ImportOptions) (*applianceSource, ImportPreview, error) {
	var pv ImportPreview
	s, err := openAppliance(src)
	if err != nil {
		return nil, pv, err
	}
	h := SystemHost()
	p, err := planImport(s.Appliance, o, h.MemTotalMiB(), runtime.NumCPU())
	if err != nil {
		return nil, pv, err
	}
	folder := o.Folder
	if folder == "" {
		folder = freeFolder(VMFolder(p.Config.Name))
	} else if folder, err = (Config{Workspace: folder}).WorkspaceDir(); err != nil { // "~" expanded, absolute only
		return nil, pv, err
	} else if _, err := os.Stat(folder); err == nil {
		return nil, pv, fmt.Errorf("%s already exists: choose another folder", folder)
	}
	p.Config.Workspace = folder
	if err := validatePlan(p.Config); err != nil {
		return nil, pv, fmt.Errorf("the imported VM would not be valid: %w", err)
	}
	if s.Manifest == "" {
		p.Notes = append(p.Notes, "the appliance has no manifest: its files' integrity cannot be checked")
	}
	pv = ImportPreview{Config: p.Config, Folder: folder, Notes: p.Notes, NeedBytes: s.diskBytes() + extractReserve}
	for i, d := range p.Disks {
		pv.Disks = append(pv.Disks, fmt.Sprintf("%s: %.1f GiB, %s (from %s)", d.Path, float64(d.Capacity)/(1<<30), strings.ToUpper(p.Config.Disks[i].Bus), d.Href))
	}
	parent := filepath.Dir(folder)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, pv, err
	}
	if free, err := freeBytes(parent); err == nil && free < pv.NeedBytes {
		return nil, pv, fmt.Errorf("the import needs %.1f GiB free in %s to extract the appliance, and %.1f GiB are free",
			float64(pv.NeedBytes)/(1<<30), parent, float64(free)/(1<<30))
	}
	return s, pv, nil
}

// freeFolder is dir, or dir-2, dir-3... the first that does not exist.
func freeFolder(dir string) string {
	for i := 1; ; i++ {
		d := dir
		if i > 1 {
			d = fmt.Sprintf("%s-%d", dir, i)
		}
		if _, err := os.Lstat(d); errors.Is(err, os.ErrNotExist) {
			return d
		}
	}
}

// ImportAppliance imports an .ova or .ovf as a new Linux, Windows or Other VM (design D6-D8):
// files extracted into a staging folder beside the destination with their digests checked,
// each disk's header checked, then inspected and converted to qcow2 by qemu-img with its
// declared format, the folder renamed into place and the VM registered last. Any failure or
// cancellation removes what it wrote. progress gets bytes done of the total.
func ImportAppliance(ctx context.Context, src string, o ImportOptions, progress func(done, total int64)) (VMEntry, error) {
	s, pv, err := planAppliance(src, o)
	if err != nil {
		return VMEntry{}, err
	}
	p, err := planImport(s.Appliance, o, SystemHost().MemTotalMiB(), runtime.NumCPU())
	if err != nil {
		return VMEntry{}, err
	}
	p.Config = pv.Config
	stage, err := os.MkdirTemp(filepath.Dir(pv.Folder), ".import-")
	if err != nil {
		return VMEntry{}, err
	}
	owned := stage // removed on any failure until the VM is registered
	defer func() {
		if owned != "" {
			os.RemoveAll(owned)
		}
	}()
	if err := os.Chmod(stage, 0o755); err != nil {
		return VMEntry{}, err
	}
	total := s.diskBytes() * 2 // extraction, then conversion
	var done int64
	report := func(n int64) {
		done += n
		if progress != nil {
			progress(min(done, total), total)
		}
	}
	sums, err := s.extract(ctx, stage, report)
	if err != nil {
		return VMEntry{}, err
	}
	if err := s.checkManifest(stage, sums); err != nil {
		return VMEntry{}, err
	}
	for _, d := range p.Disks {
		in := filepath.Join(stage, d.Href)
		size := s.Sizes[d.Href]
		if err := convertImported(ctx, in, d, filepath.Join(stage, d.Path), func(f float64) {
			if progress != nil {
				progress(min(done+int64(f*float64(size)), total), total)
			}
		}); err != nil {
			return VMEntry{}, fmt.Errorf("%s: %w", d.Href, err)
		}
		report(size)
		if err := os.Remove(in); err != nil {
			return VMEntry{}, err
		}
	}
	for _, name := range []string{s.DescName, s.Manifest} {
		if name != "" {
			os.Remove(filepath.Join(stage, name))
		}
	}
	if err := ctx.Err(); err != nil {
		return VMEntry{}, err
	}
	if err := os.Rename(stage, pv.Folder); err != nil {
		return VMEntry{}, fmt.Errorf("moving the imported VM into %s: %w", pv.Folder, err)
	}
	owned = pv.Folder
	e, err := createImported(p.Config)
	if err != nil {
		return VMEntry{}, err
	}
	owned = ""
	return e, nil
}

// extract runs pass 2 (D6): every referenced file and the manifest into dir, hashing each; the
// descriptor's digests come from the copy pass 1 read.
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
			f, err := os.Open(filepath.Join(filepath.Dir(s.Path), name))
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
	f, err := os.Open(s.Path)
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
// descriptor and every referenced file, each matching.
func (s *applianceSource) checkManifest(dir string, sums map[string][2]string) error {
	if s.Manifest == "" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(dir, s.Manifest))
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(b))
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

// checkDiskHeader reads an extracted disk's header itself before qemu-img opens it (D7): qemu-img
// opens a VMDK's extents and a qcow2's backing file while reading metadata.
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
		descOff, descSize := binary.LittleEndian.Uint64(hdr[28:36]), binary.LittleEndian.Uint64(hdr[36:44])
		if descOff == 0 {
			return nil // no embedded descriptor: one extent, this file
		}
		if descSize == 0 || descSize > 128 { // sectors: 64 KiB is far more than any descriptor needs
			return errors.New("its embedded descriptor has an invalid size")
		}
		desc := make([]byte, descSize*512)
		if _, err := f.ReadAt(desc, int64(descOff)*512); err != nil {
			return fmt.Errorf("reading its embedded descriptor: %w", err)
		}
		text := string(bytes.TrimRight(desc, "\x00"))
		ct := regexp.MustCompile(`(?m)^\s*createType\s*=\s*"([^"]*)"`).FindStringSubmatch(text)
		if ct == nil || (ct[1] != "monolithicSparse" && ct[1] != "streamOptimized") {
			return errors.New("it is not a single-file VMDK (sparse or streamOptimized): refusing it")
		}
		if ext := regexp.MustCompile(`(?m)^\s*(RW|RDONLY|NOACCESS)\s`).FindAllString(text, -1); len(ext) != 1 {
			return fmt.Errorf("its descriptor lists %d extents: only single-file VMDKs are accepted", len(ext))
		}
		return nil
	}
	return fmt.Errorf("format %q is not supported", format)
}

// qemuImgLimited runs qemu-img under prlimit (D7): an address space bound, and a time bound for
// quick commands (0: none).
func qemuImgLimited(ctx context.Context, asBytes int64, seconds int, args ...string) ([]byte, error) {
	name, full := "qemu-img", args
	if p, err := exec.LookPath("prlimit"); err == nil { // util-linux: on every supported distribution
		pre := []string{"--as=" + strconv.FormatInt(asBytes, 10)}
		if seconds > 0 {
			pre = append(pre, "--cpu="+strconv.Itoa(seconds))
		}
		name, full = p, append(append(pre, "--", "qemu-img"), args...)
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

// convertImported checks an extracted disk (D7) and converts it to qcow2 at out.
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
	if _, err := qemuImgLimited(ctx, 4<<30, 0, "convert", "-f", d.Format, "-O", "qcow2", "-o", "compat=1.1", in, out); err != nil {
		os.Remove(out)
		return err
	}
	if progress != nil {
		progress(1)
	}
	if err := checkStandaloneDisk(out); err != nil {
		os.Remove(out)
		return err
	}
	return nil
}
