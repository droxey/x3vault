package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTransportStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(Status{
			Version: "1.0",
			Device:  "X3",
			IP:      "192.168.1.50",
			Mode:    "transfer",
		})
	}))
	defer srv.Close()

	tr := NewTransport(srv.URL, 5*time.Second)
	st, err := tr.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Device != "X3" || st.IP != "192.168.1.50" || st.Mode != "transfer" {
		t.Fatalf("status = %+v", st)
	}
}

func TestTransportMkdirVerifiesExistingDirectory(t *testing.T) {
	for _, directory := range []bool{true, false} {
		t.Run(fmt.Sprint(directory), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/mkdir" {
					http.Error(w, "already exists", http.StatusConflict)
					return
				}
				if r.URL.Path == "/api/files" {
					_ = json.NewEncoder(w).Encode([]FileEntry{{Name: "wiki", IsDirectory: directory}})
					return
				}
				http.NotFound(w, r)
			}))
			defer srv.Close()
			err := NewTransport(srv.URL, time.Second).Mkdir(context.Background(), "/ereader", "wiki")
			if (err == nil) != directory {
				t.Fatalf("directory=%v mkdir error=%v", directory, err)
			}
		})
	}
}

func TestTransportReadFileDistinguishesMissingFromFailure(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "failure", status) }))
			defer srv.Close()
			_, err := NewTransport(srv.URL, time.Second).ReadFile(context.Background(), "/missing")
			if err == nil || errors.Is(err, fs.ErrNotExist) != (status == http.StatusNotFound) {
				t.Fatalf("HTTP %d error=%v", status, err)
			}
		})
	}
}

func TestTransportListRejectsUncertainDirectoryPayloads(t *testing.T) {
	for _, body := range []string{"null", "[] {}", "[] trailing"} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) }))
			defer srv.Close()
			if _, err := NewTransport(srv.URL, time.Second).List(context.Background(), "/ereader"); err == nil {
				t.Fatalf("accepted uncertain directory payload %q", body)
			}
		})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "[] \n\t") }))
	defer srv.Close()
	entries, err := NewTransport(srv.URL, time.Second).List(context.Background(), "/ereader")
	if err != nil || entries == nil || len(entries) != 0 {
		t.Fatalf("valid empty directory rejected: entries=%v err=%v", entries, err)
	}
}

func TestDeviceInitDoesNotClaimUncertainDirectoryResponse(t *testing.T) {
	for _, body := range []string{"null", "[] {}"} {
		t.Run(body, func(t *testing.T) {
			mutations := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					mutations++
					w.WriteHeader(http.StatusOK)
					return
				}
				if r.URL.Path == "/api/files" && r.URL.Query().Get("path") == "/" {
					_ = json.NewEncoder(w).Encode([]FileEntry{{Name: "ereader", IsDirectory: true}})
					return
				}
				if r.URL.Path == "/api/files" {
					_, _ = io.WriteString(w, body)
					return
				}
				http.NotFound(w, r)
			}))
			defer srv.Close()
			err := DeviceInit(context.Background(), NewTransport(srv.URL, time.Second), "/ereader", "ereader")
			if err == nil || mutations != 0 {
				t.Fatalf("uncertain response claimed root: error=%v mutations=%d", err, mutations)
			}
		})
	}
}
