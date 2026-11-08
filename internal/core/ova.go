package core

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	Appliance  appliance
}

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
func openAppliance(src string) (*applianceSource, error) {
	s := &applianceSource{Path: src, Sizes: map[string]int64{}}
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
	for name := range s.Sizes {
		if name == s.DescName || referenced[name] {
			continue
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
	f, err := os.Open(s.Path)
	if err != nil {
		return err
	}
	defer f.Close()
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

// walkOVF reads an .ovf and lists its folder's files the descriptor may reference; each must be a
// plain file (no link).
func (s *applianceSource) walkOVF() error {
	st, err := os.Lstat(s.Path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() > maxDescriptor {
		return errors.New("the descriptor is not a plain file of at most 4 MiB")
	}
	if s.Descriptor, err = os.ReadFile(s.Path); err != nil {
		return err
	}
	s.DescName = filepath.Base(s.Path)
	s.Sizes[s.DescName] = st.Size()
	a, err := readOVF(s.Descriptor)
	if err != nil {
		return fmt.Errorf("%s: %w", s.DescName, err)
	}
	dir := filepath.Dir(s.Path)
	names := []string{strings.TrimSuffix(s.DescName, filepath.Ext(s.DescName)) + ".mf"}
	for _, f := range a.Files {
		names = append(names, f.Href)
	}
	for i, n := range names {
		if err := memberName(n); err != nil {
			return err
		}
		st, err := os.Lstat(filepath.Join(dir, n))
		if i == 0 && errors.Is(err, os.ErrNotExist) {
			continue // no manifest
		}
		if err != nil {
			continue // a missing referenced file is reported by openAppliance
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("%s is not a plain file (a link or a device): refusing it", n)
		}
		if err := s.add(n, st.Size()); err != nil {
			return err
		}
	}
	return nil
}

// diskBytes is the size of the files the import extracts: every referenced file.
func (s *applianceSource) diskBytes() int64 {
	var n int64
	for _, f := range s.Appliance.Files {
		n += s.Sizes[f.Href]
	}
	return n
}

// extractReserve is kept free besides an import's files.
const extractReserve = 1 << 30
