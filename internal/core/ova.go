package core

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// maxMembers bounds an appliance's files (spec: refused over 64).
const maxMembers = 64

// applianceSource is an appliance whose layout passed pass 1 (design D6): its descriptor, the
// VM it describes, and the size of every file it references. Nothing has been written yet.
type applianceSource struct {
	Path       string // the .ova, or the .ovf
	OVA        bool
	Descriptor []byte
	DescName   string
	Manifest   string // the manifest's name, "" when there is none
	Sizes      map[string]int64
	Infos      map[string]os.FileInfo // the .ova, or an .ovf's files, as pass 1 saw them: pass 2 opens the same ones
	Dir        *os.File               // the appliance's folder, held open: every file is opened relative to it
	Appliance  appliance
}

// Close releases the appliance's folder.
func (s *applianceSource) Close() {
	if s != nil && s.Dir != nil {
		s.Dir.Close()
	}
}

// maxSmallFile bounds a manifest or certificate.
const maxSmallFile = 1 << 20

// memberName checks a member or referenced file name: flat, relative, no "..".
func memberName(n string) error {
	if n == "" || n == "." || n == ".." || strings.ContainsAny(n, `/\`) || strings.Contains(n, "..") || filepath.IsAbs(n) || strings.ContainsRune(n, 0) {
		return fmt.Errorf("the appliance has a file named %q, which is not a plain file name: refusing it", n)
	}
	return nil
}

// openAppliance runs pass 1 (D6) on an .ova or an .ovf: member names and types, duplicates, the
// number of files, the descriptor first and at most 4 MiB, every referenced file present and no
// other file than a manifest or certificate. It reads headers only (a regular file is seeked
// past each member's data).
func openAppliance(src string) (_ *applianceSource, err error) {
	s := &applianceSource{Path: src, Sizes: map[string]int64{}, Infos: map[string]os.FileInfo{}}
	// The folder is opened once: a folder swapped for a link later cannot redirect any file.
	if s.Dir, err = os.OpenFile(filepath.Dir(src), os.O_RDONLY|unix.O_DIRECTORY, 0); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	switch strings.ToLower(filepath.Ext(src)) {
	case ".ova":
		s.OVA = true
		if err := s.walkOVA(); err != nil {
			return nil, err
		}
	case ".ovf":
		if err := s.walkOVF(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%s is not an .ova or .ovf file", filepath.Base(src))
	}
	a, err := readOVF(s.Descriptor)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.DescName, err)
	}
	s.Appliance = a
	referenced := map[string]bool{}
	for _, f := range a.Files {
		if err := memberName(f.Href); err != nil {
			return nil, err
		}
		if referenced[f.Href] {
			return nil, fmt.Errorf("the descriptor lists %s twice", f.Href)
		}
		referenced[f.Href] = true
		size, ok := s.Sizes[f.Href]
		if !ok {
			return nil, fmt.Errorf("the appliance lacks %s, which its descriptor references", f.Href)
		}
		if f.Size >= 0 && f.Size != size {
			return nil, fmt.Errorf("%s is %d bytes, its descriptor says %d", f.Href, size, f.Size)
		}
	}
	for name, size := range s.Sizes {
		if name == s.DescName || referenced[name] {
			continue
		}
		if size > maxSmallFile {
			return nil, fmt.Errorf("%s is larger than 1 MiB: refusing it", name)
		}
		switch strings.ToLower(filepath.Ext(name)) {
		case ".mf":
			if s.Manifest != "" {
				return nil, errors.New("the appliance has two manifests")
			}
			s.Manifest = name
		case ".cert":
		default:
			return nil, fmt.Errorf("the appliance has a file %s that its descriptor does not reference: refusing it", name)
		}
	}
	return s, nil
}

func (s *applianceSource) add(name string, size int64) error {
	if err := memberName(name); err != nil {
		return err
	}
	if _, dup := s.Sizes[name]; dup {
		return fmt.Errorf("the appliance has %s twice", name)
	}
	if len(s.Sizes) >= maxMembers {
		return fmt.Errorf("the appliance has more than %d files", maxMembers)
	}
	s.Sizes[name] = size
	return nil
}

// walkOVA reads an .ova's headers; the first member must be the descriptor.
func (s *applianceSource) walkOVA() error {
	f, st, err := openPlainAt(s.Dir, filepath.Base(s.Path), nil)
	if err != nil {
		return err
	}
	defer f.Close()
	s.Infos[""] = st
	tr := tar.NewReader(f)
	for first := true; ; first = false {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%s is not a valid .ova (tar): %w", filepath.Base(s.Path), err)
		}
		if h.Typeflag != tar.TypeReg {
			return fmt.Errorf("the appliance's member %q is not a plain file: refusing it", h.Name)
		}
		if err := s.add(h.Name, h.Size); err != nil {
			return err
		}
		if first {
			if strings.ToLower(filepath.Ext(h.Name)) != ".ovf" {
				return fmt.Errorf("the .ova's first file is %s, not its descriptor (.ovf)", h.Name)
			}
			if h.Size > maxDescriptor {
				return errors.New("the descriptor is larger than 4 MiB")
			}
			if s.Descriptor, err = io.ReadAll(io.LimitReader(tr, maxDescriptor+1)); err != nil {
				return err
			}
			s.DescName = h.Name
		}
	}
	if s.Descriptor == nil {
		return errors.New("the .ova is empty")
	}
	return nil
}

// openPlainAt opens name in dir without following a symbolic link and without blocking (a FIFO
// cannot hang it) and checks it is a plain file; with want set, it must be that very file (same
// device and inode), so a file swapped since pass 1 is refused.
func openPlainAt(dir *os.File, name string, want os.FileInfo) (*os.File, os.FileInfo, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ELOOP) {
		return nil, nil, fmt.Errorf("%s is not a plain file (a link): refusing it", name)
	}
	if err != nil {
		return nil, nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	f := os.NewFile(uintptr(fd), name)
	st, err := f.Stat()
	if err == nil && !st.Mode().IsRegular() {
		err = fmt.Errorf("%s is not a plain file (a link or a device): refusing it", name)
	}
	if err == nil && want != nil && (!os.SameFile(st, want) || st.Size() != want.Size()) {
		err = fmt.Errorf("%s changed since the appliance was checked", name)
	}
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, st, nil
}

// walkOVF reads an .ovf and lists its folder's files the descriptor may reference; each must be a
// plain file (no link), and is remembered so pass 2 opens the same one.
func (s *applianceSource) walkOVF() error {
	f, st, err := openPlainAt(s.Dir, filepath.Base(s.Path), nil)
	if err != nil {
		return err
	}
	defer f.Close()
	if st.Size() > maxDescriptor {
		return errors.New("the descriptor is larger than 4 MiB")
	}
	if s.Descriptor, err = io.ReadAll(io.LimitReader(f, maxDescriptor)); err != nil {
		return err
	}
	s.DescName = filepath.Base(s.Path)
	s.Sizes[s.DescName] = st.Size()
	a, err := readOVF(s.Descriptor)
	if err != nil {
		return fmt.Errorf("%s: %w", s.DescName, err)
	}
	names := []string{strings.TrimSuffix(s.DescName, filepath.Ext(s.DescName)) + ".mf"}
	for _, f := range a.Files {
		names = append(names, f.Href)
	}
	for _, n := range names {
		if err := memberName(n); err != nil {
			return err
		}
		g, lst, err := openPlainAt(s.Dir, n, nil)
		if errors.Is(err, os.ErrNotExist) {
			continue // no manifest; a missing referenced file is reported by openAppliance
		}
		if err != nil {
			return err
		}
		g.Close()
		if err := s.add(n, lst.Size()); err != nil {
			return err
		}
		s.Infos[n] = lst
	}
	return nil
}

// extractBytes is the size of everything the import extracts (every referenced file and the
// manifest), refused when the sum overflows.
func (s *applianceSource) extractBytes() (int64, error) {
	var n int64
	for _, name := range append(hrefs(s.Appliance.Files), s.Manifest) {
		size := s.Sizes[name]
		if size < 0 || n > math.MaxInt64/4-size { // callers add a reserve and double it for progress
			return 0, errors.New("the appliance's files are impossibly large")
		}
		n += size
	}
	return n, nil
}

// extractReserve is kept free besides an import's files.
const extractReserve = 1 << 30
