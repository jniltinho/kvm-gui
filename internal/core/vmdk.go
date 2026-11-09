package core

// A streamOptimized VMDK writer (VMware's Virtual Disk Format 5.0, "stream-optimized compressed
// sparse extent"): the layout a reader can take as a stream, which VirtualBox needs for disks
// inside an .ova. qemu-img's streamOptimized output keeps its grain tables at the start and has
// no markers, footer or end-of-stream marker; VirtualBox 7.2 fails on it as soon as a disk has an
// empty region (task 3.0). Here: header, descriptor, then each non-empty grain compressed behind
// a grain marker, each grain table after its grains (only tables with grains), the grain
// directory, a footer (the header with the directory's offset) and the end-of-stream marker.

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
)

const (
	vmdkSector    = 512
	vmdkGrain     = 128 // sectors per grain: 64 KiB
	vmdkGTEs      = 512 // grain table entries: a table covers 32 MiB
	vmdkOverhead  = 128 // sectors before the first grain (header, descriptor, padding)
	vmdkDescSects = 2
	markerEOS     = 0
	markerGT      = 1
	markerGD      = 2
	markerFooter  = 3
)

// vmdkHeader is the sparse extent header (VMDK 5.0), little endian, 512 bytes on disk.
func vmdkHeader(capacity uint64, gdOffset uint64) []byte {
	h := make([]byte, vmdkSector)
	le := binary.LittleEndian
	copy(h[0:4], "KDMV")
	le.PutUint32(h[4:], 3)              // version
	le.PutUint32(h[8:], 1|1<<16|1<<17)  // valid newline test, compressed grains, markers
	le.PutUint64(h[12:], capacity)      // sectors
	le.PutUint64(h[20:], vmdkGrain)     // grain size
	le.PutUint64(h[28:], 1)             // descriptor offset (sectors)
	le.PutUint64(h[36:], vmdkDescSects) // descriptor size
	le.PutUint32(h[44:], vmdkGTEs)      // entries per grain table
	le.PutUint64(h[48:], 0)             // redundant grain directory: none
	le.PutUint64(h[56:], gdOffset)      // grain directory (GD_AT_END in the header)
	le.PutUint64(h[64:], vmdkOverhead)  // overhead
	h[72] = 0                           // clean
	h[73], h[74], h[75], h[76] = '\n', ' ', '\r', '\n'
	le.PutUint16(h[77:], 1) // deflate
	return h
}

// vmdkDescriptor is the embedded text descriptor of a streamOptimized extent named name.
func vmdkDescriptor(name string, capacity uint64) []byte {
	cyl := min(capacity/(16*63), 65535)
	d := fmt.Sprintf(`# Disk DescriptorFile
version=1
CID=%08x
parentCID=ffffffff
createType="streamOptimized"

# Extent description
RW %d SPARSE "%s"

# The Disk Data Base
#DDB

ddb.virtualHWVersion = "4"
ddb.adapterType = "ide"
ddb.geometry.cylinders = "%d"
ddb.geometry.heads = "16"
ddb.geometry.sectors = "63"
`, uint32(capacity)^0x5a5a5a5a, capacity, name, cyl)
	b := make([]byte, vmdkDescSects*vmdkSector)
	copy(b, d)
	return b
}

// vmdkWriter writes sectors and knows where it is.
type vmdkWriter struct {
	w   io.Writer
	pos uint64 // in sectors
	err error
}

func (v *vmdkWriter) write(b []byte) {
	if v.err != nil {
		return
	}
	if pad := len(b) % vmdkSector; pad != 0 {
		b = append(b, make([]byte, vmdkSector-pad)...)
	}
	_, v.err = v.w.Write(b)
	v.pos += uint64(len(b) / vmdkSector)
}

// marker is a metadata marker sector: its size in sectors and its type.
func (v *vmdkWriter) marker(sectors uint64, typ uint32) {
	m := make([]byte, vmdkSector)
	binary.LittleEndian.PutUint64(m[0:], sectors)
	binary.LittleEndian.PutUint32(m[12:], typ)
	v.write(m)
}

// writeStreamVMDK writes the raw image src as a streamOptimized VMDK at dst (named name in its
// descriptor). Holes and all-zero grains are left out; ctx cancels it; progress gets the bytes
// read of the image's size.
func writeStreamVMDK(ctx context.Context, src, dst string, progress func(done, total int64)) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	size := st.Size()
	// Whole grains: VirtualBox refuses a stream whose capacity ends inside a grain (task 4.2, an
	// ISO-sized disk), so the extent is rounded up to one and the last grain padded with zeros.
	capacity := vmdkCapacity(size)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	bw := &vmdkWriter{w: out}
	bw.write(vmdkHeader(capacity, ^uint64(0)))
	bw.write(vmdkDescriptor(filepath.Base(dst), capacity))
	bw.write(make([]byte, (vmdkOverhead-1-vmdkDescSects)*vmdkSector))

	grains := (capacity + vmdkGrain - 1) / vmdkGrain
	tables := (grains + vmdkGTEs - 1) / vmdkGTEs
	gd := make([]uint32, tables)
	grain := make([]byte, vmdkGrain*vmdkSector)
	var comp bytes.Buffer
	zw := zlib.NewWriter(&comp)
	for t := uint64(0); t < tables; t++ {
		gt := make([]uint32, vmdkGTEs)
		used := false
		for e := uint64(0); e < vmdkGTEs; e++ {
			g := t*vmdkGTEs + e
			if g >= grains {
				break
			}
			off := int64(g * vmdkGrain * vmdkSector)
			if off%(64<<20) == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
				if progress != nil {
					progress(off, size)
				}
			}
			n, err := in.ReadAt(grain, off)
			if err != nil && err != io.EOF {
				return err
			}
			clear(grain[n:]) // the padding of a last, partial grain
			data := grain
			if allZero(data) {
				continue
			}
			comp.Reset()
			zw.Reset(&comp)
			zw.Write(data)
			if err := zw.Close(); err != nil {
				return err
			}
			if bw.pos > math.MaxUint32 { // grain tables hold 32-bit sector offsets: 2 TiB of stream
				return errors.New("the disk's compressed stream is over 2 TiB, which a VMDK cannot address")
			}
			gt[e] = uint32(bw.pos)
			m := make([]byte, 12, 12+comp.Len())
			binary.LittleEndian.PutUint64(m[0:], g*vmdkGrain)
			binary.LittleEndian.PutUint32(m[8:], uint32(comp.Len()))
			bw.write(append(m, comp.Bytes()...))
			used = true
		}
		if used { // a table follows its grains; an empty one is left out (its directory entry is 0)
			bw.marker(vmdkGTEs*4/vmdkSector, markerGT)
			if bw.pos > math.MaxUint32 {
				return errors.New("the disk's compressed stream is over 2 TiB, which a VMDK cannot address")
			}
			gd[t] = uint32(bw.pos)
			b := make([]byte, vmdkGTEs*4)
			for i, x := range gt {
				binary.LittleEndian.PutUint32(b[i*4:], x)
			}
			bw.write(b)
		}
	}
	gdBytes := make([]byte, len(gd)*4)
	for i, x := range gd {
		binary.LittleEndian.PutUint32(gdBytes[i*4:], x)
	}
	bw.marker(uint64((len(gdBytes)+vmdkSector-1)/vmdkSector), markerGD)
	gdAt := bw.pos
	bw.write(gdBytes)
	bw.marker(1, markerFooter)
	bw.write(vmdkHeader(capacity, gdAt))
	bw.write(make([]byte, vmdkSector)) // end of stream
	if bw.err != nil {
		return bw.err
	}
	if progress != nil {
		progress(size, size)
	}
	return out.Sync()
}

func allZero(b []byte) bool {
	for len(b) >= 8 {
		if binary.LittleEndian.Uint64(b) != 0 {
			return false
		}
		b = b[8:]
	}
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

// vmdkCapacity is a streamOptimized extent's capacity in sectors for an image of size bytes:
// rounded up to whole grains.
func vmdkCapacity(size int64) uint64 {
	grainBytes := uint64(vmdkGrain * vmdkSector)
	return (uint64(size) + grainBytes - 1) / grainBytes * vmdkGrain
}
