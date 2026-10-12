// Mcadump reads and writes region files (.mca) with the library's save/region package: it unpacks each
// chunk of a region into a file of its own (.mcc), or packs such files back into their region.
//
//	go run ./examples/mcadump r.0.0.mca          # c.<x>.<z>.mcc per chunk: the chunk as stored (compression byte, then the compressed NBT)
//	go run ./examples/mcadump -x r.0.0.mca       # c.<x>.<z>.mcc.nbt per chunk: the NBT, decompressed
//	go run ./examples/mcadump -p c.*.mcc         # the .mcc files back into r.<X>.<Z>.mca
//
// mcadump_test.go runs it on a region the save package writes (examples/README.md).
package main

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mj41/go-mc26/save/region"
)

var (
	decomp = flag.Bool("x", false, "decompress each chunk to NBT format")
	repack = flag.Bool("p", false, "repack .mcc files to .mca")
)

func main() {
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() < 1 {
		usage()
		os.Exit(2)
	}
	if err := run(flag.Args(), ".", *decomp, *repack); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	_, _ = fmt.Fprintf(os.Stderr, "usage: %s [-x] r.<X>.<Z>.mca …  |  %s -p c.<x>.<z>.mcc …\n", os.Args[0], os.Args[0])
}

// run unpacks the regions the patterns name into out (pack: the chunk files back into regions in out).
func run(patterns []string, out string, decomp, pack bool) error {
	for _, p := range patterns {
		files, err := filepath.Glob(p)
		if err != nil {
			return err
		}
		for _, f := range files {
			if pack {
				err = packChunk(f, out)
			} else {
				err = unpack(f, out, decomp)
			}
			if err != nil {
				return fmt.Errorf("%s: %w", f, err)
			}
		}
	}
	return nil
}

// unpack writes every chunk of the region file f into out: as stored, or with decomp its NBT.
func unpack(f, out string, decomp bool) error {
	var x, z int
	if _, err := fmt.Sscanf(filepath.Base(f), "r.%d.%d.mca", &x, &z); err != nil {
		return fmt.Errorf("not a region file name: %w", err)
	}
	r, err := region.OpenReadOnly(f)
	if err != nil {
		return err
	}
	defer r.Close()

	for i := 0; i < 32; i++ {
		for j := 0; j < 32; j++ {
			if !r.ExistSector(i, j) {
				continue
			}
			data, err := r.ReadSector(i, j)
			if err != nil {
				return err
			}
			fn := fmt.Sprintf("c.%d.%d.mcc", x<<5+i, z<<5+j)
			var body io.Reader = bytes.NewReader(data) // as stored: the compression byte kept, so -p can put it back
			if decomp {
				fn += ".nbt" // a standard NBT file once decompressed
				if body, err = decompress(data); err != nil {
					return err
				}
			}
			cf, err := os.OpenFile(filepath.Join(out, fn), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o666)
			if err != nil {
				return err
			}
			_, err = io.Copy(cf, body)
			if cerr := cf.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// decompress is a stored chunk's NBT: the first byte says how the rest is compressed.
func decompress(data []byte) (io.Reader, error) {
	if len(data) == 0 {
		return nil, errors.New("empty sector")
	}
	rest := bytes.NewReader(data[1:])
	switch data[0] {
	case 1:
		return gzip.NewReader(rest)
	case 2:
		return zlib.NewReader(rest)
	case 3:
		return rest, nil // uncompressed
	default:
		return nil, fmt.Errorf("unknown compression type 0x%02x", data[0])
	}
}

// packChunk writes the chunk file f (as unpack wrote it, without -x) into its region in out.
func packChunk(f, out string) error {
	var x, z int
	if _, err := fmt.Sscanf(filepath.Base(f), "c.%d.%d.mcc", &x, &z); err != nil {
		return fmt.Errorf("not a chunk file name: %w", err)
	}
	fn := filepath.Join(out, fmt.Sprintf("r.%d.%d.mca", x>>5, z>>5))
	r, err := region.Open(fn)
	if errors.Is(err, os.ErrNotExist) {
		r, err = region.Create(fn)
	}
	if err != nil {
		return err
	}
	defer r.Close()

	mcc, err := os.ReadFile(f)
	if err != nil {
		return err
	}
	rx, rz := region.In(x, z)
	return r.WriteSector(rx, rz, mcc)
}
