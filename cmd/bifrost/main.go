package main

import (
	"bifrost.local/native"
	"bifrost.local/native/internal/engine"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const localBase = "http://127.0.0.1:8765"

func dataRoot() string {
	home, _ := os.UserHomeDir()
	root := os.Getenv("XDG_DATA_HOME")
	if root == "" {
		root = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(root, "BIfrost")
}

func cacheRoot() string {
	home, _ := os.UserHomeDir()
	root := os.Getenv("XDG_CACHE_HOME")
	if root == "" {
		root = filepath.Join(home, ".cache")
	}
	return filepath.Join(root, "BIfrost")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve(os.Args[2:])
	case "health":
		err = printAPI("GET", "/api/health", nil)
	case "list":
		err = printAPI("GET", "/api/files", nil)
	case "pair":
		err = pair(os.Args[2:])
	case "revoke":
		err = printAPI("POST", "/api/local/revoke", nil)
	case "upload":
		if len(os.Args) < 3 {
			err = errors.New("use: bifrost upload PATH")
		} else {
			err = upload(os.Args[2])
		}
	case "download":
		if len(os.Args) < 3 || len(os.Args) > 4 {
			err = errors.New("use: bifrost download ID [FOLDER]")
		} else {
			folder := ""
			if len(os.Args) == 4 {
				folder = os.Args[3]
			}
			err = download(os.Args[2], folder)
		}
	case "delete":
		if len(os.Args) < 3 {
			err = errors.New("use: bifrost delete ID")
		} else {
			err = deleteFile(os.Args[2])
		}
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "BIfrost:", err)
		os.Exit(1)
	}
}
func usage() {
	fmt.Println("BIfrost — serve [--lan] | health | pair [--ip x.x.x.x] | list | upload PATH | download ID [FOLDER] | delete ID | revoke")
}

func serve(args []string) error {
	addr := "127.0.0.1:8765"
	if len(args) > 0 && args[0] == "--lan" {
		addr = "0.0.0.0:8765"
	}
	site := bifrost.WebFS()
	s := engine.New(dataRoot(), site)
	h := &http.Server{Addr: addr, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	fmt.Println("BIfrost listening on", addr)
	if addr == "0.0.0.0:8765" {
		fmt.Println("LAN enabled: use only trusted Wi-Fi; remote API requires QR pairing")
	}
	return h.ListenAndServe()
}
func api(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, localBase+path, body)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	return client.Do(req)
}
func getResponse(res *http.Response, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if e != nil {
		return nil, e
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return b, nil
}
func printAPI(method, path string, body io.Reader) error {
	b, err := getResponse(api(method, path, body))
	if err != nil {
		return err
	}
	fmt.Println(string(bytes.TrimSpace(b)))
	return nil
}

func pickLANIP() string {
	// Explicit override is best for VPNs / multiple network adapters.
	if ip := os.Getenv("BIFROST_LAN_IP"); ip != "" && net.ParseIP(ip) != nil {
		return ip
	}
	interfaces, _ := net.Interfaces()
	for _, namePattern := range []string{"wlan", "wlp", "wifi", "enp", "eth"} {
		for _, iface := range interfaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || !strings.HasPrefix(strings.ToLower(iface.Name), namePattern) {
				continue
			}
			ips, _ := iface.Addrs()
			for _, item := range ips {
				ipnet, ok := item.(*net.IPNet)
				if !ok {
					continue
				}
				ip := ipnet.IP.To4()
				if ip != nil && ip.IsPrivate() {
					return ip.String()
				}
			}
		}
	}
	return ""
}

func pair(args []string) error {
	ip := pickLANIP()
	if len(args) == 2 && args[0] == "--ip" {
		ip = args[1]
	}
	if ip == "" || net.ParseIP(ip) == nil || net.ParseIP(ip).To4() == nil {
		return errors.New("could not find LAN IPv4; run bifrost pair --ip 192.168.x.x")
	}
	data, err := getResponse(api("POST", "/api/local/pair", nil))
	if err != nil {
		return err
	}
	var resp struct {
		Code    string `json:"code"`
		Expires int    `json:"expiresInSeconds"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return err
	}
	link := "http://" + ip + ":8765/pair?code=" + url.QueryEscape(resp.Code)
	qrPath := ""
	if _, err := exec.LookPath("qrencode"); err == nil {
		if err := os.MkdirAll(cacheRoot(), 0700); err == nil {
			qrPath = filepath.Join(cacheRoot(), "pair.png")
			// Publicly readable QR would reveal the invitation: mode restricted after generation.
			if err := exec.Command("qrencode", "-o", qrPath, "-s", "5", link).Run(); err != nil {
				qrPath = ""
			} else {
				_ = os.Chmod(qrPath, 0600)
			}
		}
	}
	out := map[string]any{"url": link, "qr": qrPath, "expiresInSeconds": resp.Expires}
	return json.NewEncoder(os.Stdout).Encode(out)
}

func pathFromArg(arg string) (string, error) {
	if !strings.HasPrefix(arg, "file://") {
		return arg, nil
	}
	u, err := url.Parse(arg)
	if err != nil {
		return "", err
	}
	if u.Host != "" && u.Host != "localhost" {
		return "", errors.New("remote file URLs are not supported")
	}
	return url.PathUnescape(u.Path)
}

type progressReader struct {
	reader io.Reader
	total  int64
	done   int64
	last   int
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.reader.Read(b)
	p.done += int64(n)
	if p.total > 0 {
		percent := int(p.done * 100 / p.total)
		if percent >= p.last+5 || percent == 100 {
			p.last = percent
			fmt.Fprintf(os.Stderr, "PROGRESS %d\n", percent)
		}
	}
	return n, err
}

func upload(fileArg string) error {
	path, err := pathFromArg(fileArg)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	if !stat.Mode().IsRegular() {
		return errors.New("please select a regular file")
	}
	if stat.Size() > engine.MaxFileSize {
		return errors.New("maximum upload: 512 MiB")
	}
	name := filepath.Base(path)
	// No output progress yet; HTTP body is streamed, not fully buffered.
	req, err := http.NewRequest("POST", localBase+"/api/files?name="+url.QueryEscape(name), &progressReader{reader: f, total: stat.Size()})
	if err != nil {
		return err
	}
	req.ContentLength = stat.Size()
	client := &http.Client{Timeout: 0}
	res, err := client.Do(req)
	b, err := getResponse(res, err)
	if err != nil {
		return err
	}
	fmt.Println(strings.TrimSpace(string(b)))
	return nil
}
func download(id, chosenFolder string) error {
	// Read metadata first to determine the server-provided original filename.
	b, err := getResponse(api("GET", "/api/files", nil))
	if err != nil {
		return err
	}
	var files []engine.File
	if err := json.Unmarshal(b, &files); err != nil {
		return err
	}
	var name string
	for _, f := range files {
		if f.ID == id {
			name = f.Name
			break
		}
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return errors.New("unknown file ID")
	}
	dir := chosenFolder
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Downloads", "BIfrost")
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	} else {
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("cannot access download folder: %w", err)
		}
		if !info.IsDir() {
			return errors.New("download destination must be a folder")
		}
	}
	dest := filepath.Join(dir, name)
	// Never overwrite an existing download.
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		_, err := os.Stat(dest)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return err
		}
		dest = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
	res, err := api("GET", "/api/files/"+id+"/download", nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("server returned HTTP %d", res.StatusCode)
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, &progressReader{reader: res.Body, total: res.ContentLength})
	closeErr := out.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(dest)
		return errors.New("download interrupted")
	}
	fmt.Println(dest)
	return nil
}
func deleteFile(id string) error {
	if len(id) != 32 {
		return errors.New("invalid file ID")
	}
	data, err := getResponse(api("DELETE", "/api/files/"+id, nil))
	if err != nil {
		return err
	}
	_ = data
	fmt.Println("deleted", id)
	return nil
}
