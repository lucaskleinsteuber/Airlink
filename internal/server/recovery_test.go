package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func TestStoredTransferErrorsStayLocal(t *testing.T) {
	s := integrationHost(t)
	d, token := integrationDevice(t, s, false)
	path := filepath.Join(t.TempDir(), "private-source.txt")
	if e := os.WriteFile(path, []byte("secret"), 0600); e != nil {
		t.Fatal(e)
	}
	tr, e := s.engine.Offer(d, []string{path})
	if e != nil {
		t.Fatal(e)
	}
	_ = s.engine.Fail(tr.ID, (&os.PathError{Op: "open", Path: path, Err: errors.New("not available")}).Error())
	var result model.Transfer
	if e := requestJSON(context.Background(), pinnedClient(s.fingerprint), http.MethodGet, hostURL(s)+"/api/v1/transfers/"+tr.ID, token, nil, &result); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(result.Error, path) || result.Destination != "" {
		t.Fatal("private path leaked through persisted error")
	}
}
func TestPinnedClientNeverFollowsRedirects(t *testing.T) {
	client := pinnedClient(strings.Repeat("a", 64))
	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid", nil)
	if client.CheckRedirect == nil || client.CheckRedirect(req, nil) != http.ErrUseLastResponse {
		t.Fatal("pinned client must not follow redirects outside paired origin")
	}
}
