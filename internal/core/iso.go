package core

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

// ISOLabel reads an ISO's volume label from its ISO 9660 primary volume descriptor (sector 16),
// offline and without mounting it. Windows and most Linux installers carry one even when their
// files are on UDF.
func ISOLabel(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	pvd := make([]byte, 2048)
	if _, err := f.ReadAt(pvd, 16*2048); err != nil && err != io.EOF {
		return "", err
	}
	if pvd[0] != 1 || !bytes.Equal(pvd[1:6], []byte("CD001")) {
		return "", fmt.Errorf("%s is not an ISO 9660 image", path)
	}
	return strings.TrimRight(string(pvd[40:72]), " \x00"), nil
}

// DetectProfile proposes the OS profile of an installer ISO by its volume label; ok is false
// when the label matches no known system.
func DetectProfile(path string) (p OSProfile, label string, ok bool) {
	label, err := ISOLabel(path)
	if err != nil {
		return OSProfile{}, "", false
	}
	for _, p := range Profiles {
		for _, re := range p.ISOLabels {
			if re.MatchString(label) {
				return p, label, true
			}
		}
	}
	return OSProfile{}, label, false
}
