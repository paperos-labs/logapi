package tarfs

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

func CompressAndRemove(dataDir, date, format string) error {
	if err := CompressDir(dataDir, date, format); err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(dataDir, date))
}

func CompressDir(dataDir, date, format string) error {
	tarPath := filepath.Join(dataDir, date+".tar."+format)
	if _, err := os.Stat(tarPath); !os.IsNotExist(err) {
		return nil // Skip if tarball already exists
	}

	f, err := os.Create(tarPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var tw *tar.Writer
	switch format {
	case "gz":
		gw, err := gzip.NewWriterLevel(f, gzip.BestCompression)
		if err != nil {
			return err
		}
		defer func() { _ = gw.Close() }()
		tw = tar.NewWriter(gw)
	case "bz2":
		panic(fmt.Errorf("bzip2 has no writer"))
	case "zst":
		zw, err := zstd.NewWriter(f)
		if err != nil {
			return err
		}
		defer func() { _ = zw.Close() }()
		tw = tar.NewWriter(zw)
	case "xz":
		xw, err := xz.NewWriter(f)
		if err != nil {
			return err
		}
		defer func() { _ = xw.Close() }()
		tw = tar.NewWriter(xw)
	}
	defer func() { _ = tw.Close() }()

	root := filepath.Join(dataDir, date)
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(dataDir, path)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = relPath
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		_, err = io.Copy(tw, file)
		return err
	})
	if err != nil {
		return err
	}

	return nil
}
