package sync

import (
	"context"
	"encoding/json"
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
