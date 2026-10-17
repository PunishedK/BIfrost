package engine

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const MaxFileSize int64 = 512 << 20 // 512 MiB per file
const PairingTTL = 3 * time.Minute
const SessionTTL = 12 * time.Hour
const CookieName = "bifrost_session"

type File struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	UploadedAt time.Time `json:"uploadedAt"`
}

type invitation struct {
	Code  string
	Until time.Time
}

type Server struct {
	Root     string
	Site     fs.FS
	Now      func() time.Time
	mu       sync.Mutex
	invite   invitation
	sessions map[string]time.Time
}

func New(root string, site fs.FS) *Server {
	return &Server{Root: root, Site: site, Now: time.Now, sessions: make(map[string]time.Time)}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func validName(name string) bool {
	if name == "" || name == "." || name == ".." || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 255 || strings.ContainsAny(name, `/\`) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func token() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}
func fileID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}
func (s *Server) blobs() string             { return filepath.Join(s.Root, "files") }
func (s *Server) metaPath(id string) string { return filepath.Join(s.blobs(), id+".json") }
func (s *Server) dataPath(id string) string { return filepath.Join(s.blobs(), id+".bin") }

func (s *Server) getMeta(id string) (File, error) {
	var meta File
	if !validID(id) {
		return meta, errors.New("bad id")
	}
	b, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(b, &meta); err != nil || meta.ID != id || !validName(meta.Name) {
		return meta, errors.New("bad metadata")
	}
	return meta, nil
}

func (s *Server) allFiles(w http.ResponseWriter, r *http.Request) {
	result := make([]File, 0)
	entries, err := os.ReadDir(s.blobs())
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, 200, result)
		return
	}
	if err != nil {
		fail(w, 500, "storage unavailable")
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		meta, err := s.getMeta(id)
		if err != nil {
			continue
		}
		if _, err := os.Stat(s.dataPath(id)); err == nil {
			result = append(result, meta)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UploadedAt.After(result[j].UploadedAt) })
	writeJSON(w, 200, result)
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if !validName(name) {
		fail(w, 400, "invalid filename")
		return
	}
	if r.ContentLength > MaxFileSize {
		fail(w, 413, "file exceeds 512 MiB")
		return
	}
	if err := os.MkdirAll(s.blobs(), 0700); err != nil {
		fail(w, 500, "storage unavailable")
		return
	}

	tmp, err := os.CreateTemp(s.blobs(), ".incoming-*")
	if err != nil {
		fail(w, 500, "cannot create temporary file")
		return
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		fail(w, 500, "cannot protect temporary file")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxFileSize)
	size, copyErr := io.Copy(tmp, r.Body)
	closeErr := tmp.Close()
	if copyErr != nil {
		var limit *http.MaxBytesError
		if errors.As(copyErr, &limit) {
			fail(w, 413, "file exceeds 512 MiB")
		} else {
			fail(w, 400, "upload interrupted")
		}
		return
	}
	if closeErr != nil {
		fail(w, 500, "could not save file")
		return
	}

	id, err := fileID()
	if err != nil {
		fail(w, 500, "could not create file ID")
		return
	}
	dataPath := s.dataPath(id)
	if err := os.Rename(tmp.Name(), dataPath); err != nil {
		fail(w, 500, "could not save file")
		return
	}
	meta := File{ID: id, Name: name, Size: size, UploadedAt: s.Now().UTC()}
	data, err := json.Marshal(meta)
	if err == nil {
		err = os.WriteFile(s.metaPath(id), data, 0600)
	}
	if err != nil {
		_ = os.Remove(dataPath)
		fail(w, 500, "could not save metadata")
		return
	}
	writeJSON(w, 201, meta)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	meta, err := s.getMeta(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(s.dataPath(meta.ID))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(meta.Name))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, meta.Name, meta.UploadedAt, f)
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.getMeta(id); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := os.Remove(s.dataPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		fail(w, 500, "unable to remove file")
		return
	}
	if err := os.Remove(s.metaPath(id)); err != nil {
		fail(w, 500, "unable to remove metadata")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func isLocal(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	return err == nil && net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func safeOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	} // CLI, native Quickshell processes
	u, err := url.Parse(origin)
	return err == nil && u.Scheme == "http" && u.Host == r.Host
}

func (s *Server) permitted(r *http.Request) bool {
	if isLocal(r) {
		return true
	}
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expiration, ok := s.sessions[cookie.Value]
	return ok && s.Now().Before(expiration)
}
func (s *Server) protected(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !safeOrigin(r) {
			fail(w, 403, "cross-site request denied")
			return
		}
		if !s.permitted(r) {
			fail(w, 401, "pair with BIfrost first")
			return
		}
		next(w, r)
	}
}

func (s *Server) createInvitation(w http.ResponseWriter, r *http.Request) {
	if !isLocal(r) {
		fail(w, 403, "only the Arch computer can create invitations")
		return
	}
	code, err := token()
	if err != nil {
		fail(w, 500, "could not create invitation")
		return
	}
	s.mu.Lock()
	s.invite = invitation{Code: code, Until: s.Now().Add(PairingTTL)}
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"code": code, "expiresInSeconds": int(PairingTTL.Seconds())})
}

func (s *Server) redeem(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > 4096 {
		fail(w, 413, "invalid pairing request")
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		fail(w, 400, "invalid request")
		return
	}
	s.mu.Lock()
	valid := s.invite.Code != "" && s.Now().Before(s.invite.Until) && subtle.ConstantTimeCompare([]byte(body.Code), []byte(s.invite.Code)) == 1
	if !valid {
		s.mu.Unlock()
		fail(w, 403, "expired or invalid invitation")
		return
	}
	session, err := token()
	if err != nil {
		s.mu.Unlock()
		fail(w, 500, "could not create session")
		return
	}
	s.invite = invitation{}
	s.sessions[session] = s.Now().Add(SessionTTL)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: session, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(SessionTTL.Seconds())})
	writeJSON(w, 200, map[string]string{"status": "paired"})
}

func (s *Server) revokeAll(w http.ResponseWriter, r *http.Request) {
	if !isLocal(r) {
		fail(w, 403, "local only")
		return
	}
	s.mu.Lock()
	s.sessions = make(map[string]time.Time)
	s.invite = invitation{}
	s.mu.Unlock()
	writeJSON(w, 200, map[string]string{"status": "revoked"})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"service": "BIfrost", "status": "ok", "version": "1.0.0"})
	})
	mux.HandleFunc("POST /api/local/pair", s.createInvitation)
	mux.HandleFunc("POST /api/local/revoke", s.revokeAll)
	mux.HandleFunc("POST /api/pair/redeem", func(w http.ResponseWriter, r *http.Request) {
		if !safeOrigin(r) {
			fail(w, 403, "cross-site request denied")
			return
		}
		s.redeem(w, r)
	})
	mux.HandleFunc("GET /api/files", s.protected(s.allFiles))
	mux.HandleFunc("POST /api/files", s.protected(s.upload))
	mux.HandleFunc("GET /api/files/{id}/download", s.protected(s.download))
	mux.HandleFunc("DELETE /api/files/{id}", s.protected(s.delete))
	// Do not serve directory listings: only explicit embedded files.
	site := http.FileServer(http.FS(s.Site))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/pair" && r.URL.Path != "/index.html" && r.URL.Path != "/style.css" && r.URL.Path != "/app.js" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; base-uri 'none'; object-src 'none'; frame-ancestors 'none'")
		if r.URL.Path == "/pair" || r.URL.Path == "/" || r.URL.Path == "/index.html" {
			page, err := fs.ReadFile(s.Site, "index.html")
			if err != nil {
				http.Error(w, "web client unavailable", 500)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(page)
			return
		}
		site.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Do not advertise the app to external domains / prevent browser spoofed Host.
		if r.Host == "" || strings.ContainsAny(r.Host, " /\\") {
			fail(w, 400, "invalid host")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// Exported so CLI may produce file-size messages consistently.
func FormatBytes(size int64) string {
	units := []string{"B", "KiB", "MiB", "GiB"}
	value := float64(size)
	idx := 0
	for value >= 1024 && idx < len(units)-1 {
		value /= 1024
		idx++
	}
	return fmt.Sprintf("%.1f %s", value, units[idx])
}
