package sim

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestStatusAndEmptyList(t *testing.T) {
	srv := httptest.NewServer(NewServer().Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var st Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if st.Device != "X3" || st.Version == "" || !st.SDReady {
		t.Fatalf("unexpected status: %+v", st)
	}

	resp, err = http.Get(srv.URL + "/api/files?path=/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var entries []FileEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	if entries == nil {
		t.Fatal("empty list decoded as null")
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty root, got %+v", entries)
	}
}

func TestMkdirUploadListDownloadDelete(t *testing.T) {
	srv := httptest.NewServer(NewServer().Handler())
	t.Cleanup(srv.Close)

	resp, err := http.PostForm(srv.URL+"/mkdir", url.Values{"name": {"ereader"}, "path": {"/"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("mkdir %d", resp.StatusCode)
	}

	if err := postFile(srv.URL, "/ereader", "index.md", []byte("# Index\n")); err != nil {
		t.Fatal(err)
	}

	resp, err = http.Get(srv.URL + "/api/files?path=/ereader")
	if err != nil {
		t.Fatal(err)
	}
	var entries []FileEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(entries) != 1 || entries[0].Name != "index.md" || entries[0].IsDirectory {
		t.Fatalf("list: %+v", entries)
	}

	resp, err = http.Get(srv.URL + "/download?path=/ereader/index.md")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(data) != "# Index\n" {
		t.Fatalf("download %q", data)
	}

	resp, err = http.PostForm(srv.URL+"/delete", url.Values{"path": {"/ereader/index.md"}, "type": {"file"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("delete %d", resp.StatusCode)
	}

	resp, err = http.Get(srv.URL + "/download?path=/ereader/index.md")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 got %d", resp.StatusCode)
	}
}

func TestDeleteNonEmptyDirFails(t *testing.T) {
	s := NewServer()
	if err := s.Store.Mkdir("/", "ereader"); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Upload("/ereader", "a.md", []byte("a")); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.PostForm(srv.URL+"/delete", url.Values{"path": {"/ereader"}, "type": {"directory"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status %d body %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "not empty") {
		t.Fatalf("body %s", body)
	}
}

func TestRejectsInvalidPaths(t *testing.T) {
	srv := httptest.NewServer(NewServer().Handler())
	t.Cleanup(srv.Close)

	resp, err := http.PostForm(srv.URL+"/mkdir", url.Values{"name": {".."}, "path": {"/"}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("mkdir .. status %d", resp.StatusCode)
	}
}

func TestMissingDirListIs404(t *testing.T) {
	srv := httptest.NewServer(NewServer().Handler())
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/api/files?path=/nope")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("got %d", resp.StatusCode)
	}
}

func postFile(base, dir, name string, data []byte) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", name)
	if err != nil {
		return err
	}
	if _, err := part.Write(data); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	resp, err := http.Post(base+"/upload?path="+url.QueryEscape(dir), w.FormDataContentType(), &buf)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return &statusError{code: resp.StatusCode, body: string(body)}
	}
	return nil
}

type statusError struct {
	code int
	body string
}

func (e *statusError) Error() string {
	return e.body
}
