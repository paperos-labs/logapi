package logapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/paperos-labs/logapi/tarfs"
)

type BasicAuthVerifier interface {
	Verify(string, string) bool
}

// Server holds application state
type Server struct {
	auth      BasicAuthVerifier
	storage   string
	compress  string
	tarFS     map[string]*tarfs.TarFS // date -> TarFS
	tarFSLock sync.RWMutex
}

// JSONError represents an API error response
type JSONError struct {
	Error  string `json:"error"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// Request represents the POST /api/logs JSON body
type Request struct {
	Date string `json:"date"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// New initializes the server
func New(auth BasicAuthVerifier, storage string, compress string) (*Server, error) {
	if compress != "zst" && compress != "gz" && compress != "xz" {
		return nil, fmt.Errorf("unsupported compression format: %s", compress)
	}

	server := &Server{
		auth:     auth,
		storage:  storage,
		compress: compress,
		tarFS:    make(map[string]*tarfs.TarFS),
	}
	return server, nil
}

// jsonError writes a JSON error response
func (s *Server) jsonError(w http.ResponseWriter, status int, code, errorMsg, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	_ = enc.Encode(JSONError{
		Error:  errorMsg,
		Code:   code,
		Detail: detail,
	})
}

func (s *Server) UploadLog(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok || !s.auth.Verify(username, password) {
		s.jsonError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized", "Invalid credentials")
		return
	}

	date := r.Header.Get("X-File-Date")
	name := r.Header.Get("X-File-Name")
	if date == "" || name == "" {
		s.jsonError(w, http.StatusBadRequest, "missing_headers", "Missing headers", "X-File-Date and X-File-Name are required")
		return
	}

	// Validate date (YYYY-MM, within 10 days, UTC)
	dateTime, err := time.Parse("2006-01", date)
	if err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid_date", "Invalid date format", "X-File-Date must be YYYY-MM")
		return
	}
	now := time.Now().UTC()
	firstOfCurrentMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	firstOfLastMonth := firstOfCurrentMonth.AddDate(0, -1, 0)
	tomorrow := now.AddDate(0, 0, 1)
	if dateTime.Before(firstOfLastMonth) || dateTime.After(tomorrow) {
		s.jsonError(
			w,
			http.StatusBadRequest,
			"date_out_of_range",
			"Date out of range",
			fmt.Sprintf(
				"Date must be between %s and %s, but got %s (%s)",
				firstOfLastMonth.Format("2006-01-02 15:04:05"),
				tomorrow.Format("2006-01-02 15:04:05"),
				now.Format("2006-01"),
				now.Format("2006-01 15:04:05"),
			),
		)
		return
	}

	dataDir := filepath.Join(s.storage, username, date)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "server_error", "Server error", err.Error())
		return
	}
	storagePath := filepath.Join(dataDir, name)

	tmpPath := storagePath + ".tmp"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "server_error", "Server error", err.Error())
		return
	}
	defer func() { _ = tmpFile.Close() }()

	if _, err := io.Copy(tmpFile, r.Body); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "write_failed", "Failed to write file", err.Error())
		return
	}

	if err := os.Rename(tmpPath, storagePath); err != nil {
		s.jsonError(w, http.StatusInternalServerError, "server_error", "Server error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]string{
		"message": fmt.Sprintf("File uploaded: %s", r.URL.Path),
	})
}

func (s *Server) ListMonths(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok || !s.auth.Verify(username, password) {
		s.jsonError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized", "Invalid credentials")
		return
	}

	user := r.PathValue("user")
	if username != user {
		s.jsonError(w, http.StatusForbidden, "forbidden", "Forbidden", "You can only access your own files")
		return
	}

	userDir := filepath.Join(s.storage, username)
	monthEntries, err := os.ReadDir(userDir)
	if err != nil {
		s.jsonError(w, http.StatusInternalServerError, "server_error", "Server error", err.Error())
		return
	}

	var months []string
	for _, monthEntry := range monthEntries {
		name := monthEntry.Name()
		if !monthEntry.IsDir() {
			// remove .tar.zstd
			ext := filepath.Ext(name)
			name = strings.TrimSuffix(name, ext)
			ext = filepath.Ext(name)
			name = strings.TrimSuffix(name, ext)
		}

		if _, err := time.Parse("2006-01", name); err != nil {
			continue
		}

		months = append(months, name)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]any{
		"results": months,
	})
}

func (s *Server) ListFiles(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok || !s.auth.Verify(username, password) {
		s.jsonError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized", "Invalid credentials")
		return
	}

	user := r.PathValue("user")
	if username != user {
		s.jsonError(w, http.StatusForbidden, "forbidden", "Forbidden", "You can only access your own files")
		return
	}
	date := r.PathValue("date")

	var filenames []string
	dateDir := filepath.Join(s.storage, user, date)
	entries, err := os.ReadDir(dateDir)
	if err != nil {
		s.tarFSLock.RLock()
		tfs, ok := s.tarFS[date]
		s.tarFSLock.RUnlock()
		if !ok {
			tarPath := filepath.Join(s.storage, user, date+".tar."+s.compress)
			var err error
			tfs, err = tarfs.NewTarFS(tarPath)
			if err != nil {
				s.jsonError(w, http.StatusNotFound, "file_not_found", "File not found", err.Error())
				return
			}
			s.tarFSLock.Lock()
			s.tarFS[date] = tfs
			s.tarFSLock.Unlock()
		}

		paths := tfs.EntryPaths()
		for _, path := range paths {
			filenames = append(filenames, strings.TrimPrefix(path, date+"/"))
		}
	}
	for _, entry := range entries {
		filenames = append(filenames, entry.Name())
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	enc := json.NewEncoder(w)
	_ = enc.Encode(map[string]any{
		"results": filenames,
	})
}

func (s *Server) GetFile(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok || !s.auth.Verify(username, password) {
		s.jsonError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized", "Invalid credentials")
		return
	}

	user := r.PathValue("user")
	if username != user {
		s.jsonError(w, http.StatusForbidden, "forbidden", "Forbidden", "You can only access your own files")
		return
	}
	date := r.PathValue("date")
	name := r.PathValue("name")

	// Validate date format
	if _, err := time.Parse("2006-01", date); err != nil {
		s.jsonError(w, http.StatusBadRequest, "invalid_date", "Invalid date format", "Date must be YYYY-MM")
		return
	}

	// Check filesystem first
	filePath := filepath.Join(s.storage, user, date, name)
	if f, err := os.Open(filePath); err == nil {
		_, _ = io.Copy(w, f)
		return
	}

	// Try streaming from tarball
	s.tarFSLock.RLock()
	tfs, ok := s.tarFS[date]
	s.tarFSLock.RUnlock()
	if !ok {
		tarPath := filepath.Join(s.storage, user, date+".tar."+s.compress)
		var err error
		tfs, err = tarfs.NewTarFS(tarPath)
		if err != nil {
			s.jsonError(w, http.StatusNotFound, "file_not_found", "File not found", err.Error())
			return
		}
		s.tarFSLock.Lock()
		s.tarFS[date] = tfs
		s.tarFSLock.Unlock()
	}

	f, err := tfs.Get(filepath.Join(date, name))
	if err != nil {
		s.jsonError(w, http.StatusNotFound, "file_not_found", "File not found", err.Error())
		return
	}
	_, _ = io.Copy(w, f)
}

func (s *Server) CompressAll(now time.Time, stale time.Duration) ([]string, error) {
	var tarballs []string

	then := now.Add(-stale)
	thenName := then.Format("2006-01")

	userDirs, err := os.ReadDir(s.storage)
	if err != nil {
		return nil, err
	}
	for _, userDir := range userDirs {
		if !userDir.IsDir() {
			continue
		}

		userPath := filepath.Join(s.storage, userDir.Name())
		dateDirs, err := os.ReadDir(userPath)
		if err != nil {
			continue
		}
		for _, dateDir := range dateDirs {
			if !dateDir.IsDir() {
				continue
			}

			dateName := dateDir.Name()
			if _, err := time.Parse("2006-01", dateName); err != nil {
				continue
			}

			if dateName >= thenName {
				continue
			}

			// TODO Compress(root, dirs, format)
			if err := tarfs.CompressAndRemove(userPath, dateName, s.compress); err != nil {
				return nil, err
			}

			tarball := filepath.Join(userPath, dateName+".tar."+s.compress)
			tarballs = append(tarballs, tarball)
		}
	}

	return tarballs, nil
}
