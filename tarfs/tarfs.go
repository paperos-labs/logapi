package tarfs

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// TarFS is a streaming virtual filesystem for tar archives
type TarFS struct {
	path    string
	indices map[string]int // last wins
	sizes   map[string]int64
	format  string
}

// NewTarFS scans a tar archive to index file offsets and sizes
func NewTarFS(path string) (*TarFS, error) {
	format := detectFormat(path)
	if format == "" {
		return nil, fmt.Errorf("unsupported file format: %s", path)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	tr, err := newTarReader(f, format)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tr.Close() }()

	fs := &TarFS{
		path:    path,
		indices: make(map[string]int),
		sizes:   make(map[string]int64),
		format:  format,
	}
	tarReader := tar.NewReader(tr)

	for i := 0; true; i++ {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// fmt.Println("[tarfs] HEAD", hdr.Name)
		if hdr.Typeflag == tar.TypeReg {
			// fmt.Println("[tarfs] CACHE", hdr.Name)
			fs.indices[hdr.Name] = i
			fs.sizes[hdr.Name] = hdr.Size
			_, err = io.CopyN(io.Discard, tarReader, hdr.Size)
			if err != nil {
				return nil, err
			}
		}
	}

	return fs, nil
}

// Get fetches a specific file's contents from the tar archive
func (fs *TarFS) Get(path string) (io.Reader, error) {
	index, ok := fs.indices[path]
	if !ok {
		return nil, fmt.Errorf("file %s not found", path)
	}
	fmt.Printf("[tarfs] GET %s (%s)\n", path, fs.path)

	f, err := os.Open(fs.path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	tr, err := newTarReader(f, fs.format)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tr.Close() }()

	tarReader := tar.NewReader(tr)
	var hdr *tar.Header
	for i := 0; i <= index; i++ {
		hdr, err = tarReader.Next()
		if err != nil {
			return nil, err
		}
	}
	if hdr.Name != path {
		return nil, fmt.Errorf("expected file %s, found %s", path, hdr.Name)
	}

	return tarReader, nil
}

func (fs *TarFS) EntryPaths() []string {
	paths := slices.Collect(maps.Keys(fs.indices))

	// n := len(fs.indices)
	// infos := make([]Info, n)
	// for _, path := range paths {
	// 	i := fs.indices[path]
	// 	infos[i] = Info{name: path, size: fs.sizes[path]}
	// }

	return paths
}

// detectFormat infers compression format from file extension
func detectFormat(path string) string {
	switch filepath.Ext(path) {
	case ".zst":
		return "zst"
	case ".br":
		return "br"
	case ".gz":
		return "gz"
	case ".bz2":
		return "bz2"
	case ".xz":
		return "xz"
	default:
		return ""
	}
}

// tarReader wraps a reader with compression-specific handling
type tarReader struct {
	reader io.Reader
	closer io.Closer
}

func (tr *tarReader) Read(p []byte) (n int, err error) {
	return tr.reader.Read(p)
}

func (tr *tarReader) Close() error {
	if tr.closer != nil {
		return tr.closer.Close()
	}
	return nil
}

// newTarReader creates a reader for the specified compression format
func newTarReader(f *os.File, format string) (*tarReader, error) {
	switch format {
	case "gz":
		gr, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		return &tarReader{reader: gr, closer: gr}, nil
	case "bz2":
		return &tarReader{reader: bzip2.NewReader(f), closer: nil}, nil
	case "zst":
		zr, err := zstd.NewReader(f)
		if err != nil {
			return nil, err
		}
		return &tarReader{reader: zr, closer: nil}, nil
	case "xz":
		xr, err := xz.NewReader(f)
		if err != nil {
			return nil, err
		}
		return &tarReader{reader: xr, closer: nil}, nil
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}
