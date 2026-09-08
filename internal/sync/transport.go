package sync

// Device paths use POSIX path (path package), not host filepath.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxHTTPBody = 64 << 10

type FileEntry struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	IsDirectory bool   `json:"isDirectory"`
	IsEpub      bool   `json:"isEpub"`
}

type Status struct {
	Version  string `json:"version"`
	Device   string `json:"device"`
	IP       string `json:"ip"`
	Mode     string `json:"mode"`
	RSSI     int    `json:"rssi"`
	FreeHeap int    `json:"freeHeap"`
	Uptime   int    `json:"uptime"`
	SDReady  bool   `json:"sdReady"`
}

// FileTransport is the device file API used by sync planning and apply.
type FileTransport interface {
	Status(ctx context.Context) (*Status, error)
	List(ctx context.Context, dirPath string) ([]FileEntry, error)
	Mkdir(ctx context.Context, parent, name string) error
	Upload(ctx context.Context, dirPath, filename string, data []byte) error
	Delete(ctx context.Context, itemPath, itemType string) error
	ReadFile(ctx context.Context, itemPath string) ([]byte, error)
}

type Transport struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewTransport(baseURL string, timeout time.Duration) *Transport {
	baseURL = strings.TrimRight(baseURL, "/")
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Transport{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func readLimitedBody(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxHTTPBody))
}

func (t *Transport) Status(ctx context.Context) (*Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.BaseURL+"/api/status", nil)
	if err != nil {
		return nil, fmt.Errorf("status request: %w", err)
	}
	resp, err := t.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("status: %w (is the X3 on its File Transfer / Wi-Fi screen?)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := readLimitedBody(resp.Body)
		return nil, fmt.Errorf("status HTTP %d: %s", resp.StatusCode, body)
	}
	var s Status
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, fmt.Errorf("status decode: %w", err)
	}
	return &s, nil
}

func (t *Transport) List(ctx context.Context, dirPath string) ([]FileEntry, error) {
	if dirPath == "" {
		dirPath = "/"
	}
	u := t.BaseURL + "/api/files?path=" + url.QueryEscape(dirPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("list request: %w", err)
	}
	resp, err := t.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dirPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := readLimitedBody(resp.Body)
		return nil, fmt.Errorf("list %s HTTP %d: %s", dirPath, resp.StatusCode, body)
	}
	var entries []FileEntry
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("list decode: %w", err)
	}
	if entries == nil {
		return nil, fmt.Errorf("list decode: expected a directory array, got null")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("list trailing data: %w", err)
		}
		return nil, fmt.Errorf("list decode: unexpected value after directory array")
	}
	return entries, nil
}

func (t *Transport) Mkdir(ctx context.Context, parent, name string) error {
	if parent == "" {
		parent = "/"
	}
	form := url.Values{}
	form.Set("name", name)
	form.Set("path", parent)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.BaseURL+"/mkdir", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("mkdir request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("mkdir %s/%s: %w", parent, name, err)
	}
	defer resp.Body.Close()
	body, _ := readLimitedBody(resp.Body)
	if resp.StatusCode != http.StatusOK {
		if strings.Contains(string(body), "already exists") {
			entries, err := t.List(ctx, parent)
			if err != nil {
				return fmt.Errorf("verify existing directory: %w", err)
			}
			for _, entry := range entries {
				if entry.Name == name && entry.IsDirectory {
					return nil
				}
			}
		}
		return fmt.Errorf("mkdir %s/%s HTTP %d: %s", parent, name, resp.StatusCode, body)
	}
	return nil
}

func (t *Transport) Upload(ctx context.Context, dirPath, filename string, data []byte) error {
	if dirPath == "" {
		dirPath = "/"
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	u := t.BaseURL + "/upload?path=" + url.QueryEscape(dirPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := t.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload %s/%s: %w", dirPath, filename, err)
	}
	defer resp.Body.Close()
	body, _ := readLimitedBody(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upload %s/%s HTTP %d: %s", dirPath, filename, resp.StatusCode, body)
	}
	return nil
}

func (t *Transport) Delete(ctx context.Context, itemPath, itemType string) error {
	if itemType == "" {
		itemType = "file"
	}
	form := url.Values{}
	form.Set("path", itemPath)
	form.Set("type", itemType)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.BaseURL+"/delete", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("delete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete %s: %w", itemPath, err)
	}
	defer resp.Body.Close()
	body, _ := readLimitedBody(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete %s HTTP %d: %s", itemPath, resp.StatusCode, body)
	}
	return nil
}

func (t *Transport) ReadFile(ctx context.Context, itemPath string) ([]byte, error) {
	u := t.BaseURL + "/download?path=" + url.QueryEscape(itemPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("read request: %w", err)
	}
	resp, err := t.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", itemPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := readLimitedBody(resp.Body)
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("read %s HTTP %d: %w", itemPath, resp.StatusCode, fs.ErrNotExist)
		}
		return nil, fmt.Errorf("read %s HTTP %d: %s", itemPath, resp.StatusCode, body)
	}
	return io.ReadAll(resp.Body)
}
