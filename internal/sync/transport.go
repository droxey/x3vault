package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

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

type Transport struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewTransport(baseURL string) *Transport {
	baseURL = strings.TrimRight(baseURL, "/")
	return &Transport{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (t *Transport) Status() (*Status, error) {
	resp, err := t.HTTPClient.Get(t.BaseURL + "/api/status")
	if err != nil {
		return nil, fmt.Errorf("status: %w (is the X3 on its File Transfer / Wi-Fi screen?)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status HTTP %d: %s", resp.StatusCode, body)
	}
	var s Status
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, fmt.Errorf("status decode: %w", err)
	}
	return &s, nil
}

func (t *Transport) List(dirPath string) ([]FileEntry, error) {
	if dirPath == "" {
		dirPath = "/"
	}
	u := t.BaseURL + "/api/files?path=" + url.QueryEscape(dirPath)
	resp, err := t.HTTPClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dirPath, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list %s HTTP %d: %s", dirPath, resp.StatusCode, body)
	}
	var entries []FileEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("list decode: %w", err)
	}
	return entries, nil
}

func (t *Transport) Mkdir(parent, name string) error {
	if parent == "" {
		parent = "/"
	}
	form := url.Values{}
	form.Set("name", name)
	form.Set("path", parent)
	resp, err := t.HTTPClient.PostForm(t.BaseURL+"/mkdir", form)
	if err != nil {
		return fmt.Errorf("mkdir %s/%s: %w", parent, name, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		if strings.Contains(string(body), "already exists") {
			return nil
		}
		return fmt.Errorf("mkdir %s/%s HTTP %d: %s", parent, name, resp.StatusCode, body)
	}
	return nil
}

func (t *Transport) Upload(dirPath, filename string, data []byte) error {
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
	req, err := http.NewRequest("POST", u, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := t.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload %s/%s: %w", dirPath, filename, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("upload %s/%s HTTP %d: %s", dirPath, filename, resp.StatusCode, body)
	}
	return nil
}

func (t *Transport) Delete(itemPath, itemType string) error {
	if itemType == "" {
		itemType = "file"
	}
	form := url.Values{}
	form.Set("path", itemPath)
	form.Set("type", itemType)
	resp, err := t.HTTPClient.PostForm(t.BaseURL+"/delete", form)
	if err != nil {
		return fmt.Errorf("delete %s: %w", itemPath, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("delete %s HTTP %d: %s", itemPath, resp.StatusCode, body)
	}
	return nil
}

func (t *Transport) EnsureDir(root, relDir string) error {
	parts := strings.Split(strings.Trim(relDir, "/"), "/")
	cur := root
	for _, p := range parts {
		if p == "" {
			continue
		}
		entries, err := t.List(cur)
		if err != nil {
		}
		exists := false
		for _, e := range entries {
			if e.Name == p && e.IsDirectory {
				exists = true
				break
			}
		}
		if !exists {
			if err := t.Mkdir(cur, p); err != nil {
				return err
			}
		}
		cur = path.Join(cur, p)
	}
	return nil
}
