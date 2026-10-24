package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucasfd3/Airlink/internal/model"
)

func TestPeerConfirmationSurvivesRestart(t *testing.T) {
	source, target := integrationHost(t), integrationHost(t)
	p := source.store.snapshot()
	targetID := target.store.snapshot().ID
	outgoing, incoming := randomID(), randomID()
	if e := target.store.put(deviceRecord{Device: model.Device{ID: p.ID, Name: "Source", Kind: "engine", CanSend: true, CanReceive: true, Paired: true}, TokenHash: digest([]byte(outgoing))}); e != nil {
		t.Fatal(e)
	}
	if e := source.store.put(deviceRecord{Device: model.Device{ID: targetID, Name: "Target", Kind: "engine", Address: hostURL(target), Fingerprint: target.fingerprint, CanSend: true, CanReceive: true, Paired: true}, TokenHash: digest([]byte(incoming)), OutboundToken: outgoing, PendingReturnToken: incoming}); e != nil {
		t.Fatal(e)
	}
	opts := source.opts
	if e := source.Close(); e != nil {
		t.Fatal(e)
	}
	resumed, e := New(opts)
	if e != nil {
		t.Fatal(e)
	}
	defer resumed.Close()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		record := target.store.snapshot().Devices[p.ID]
		local := resumed.store.snapshot().Devices[targetID]
		if record.OutboundToken == incoming && local.PendingReturnToken == "" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("reciprocal pairing credential was not recovered")
}
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
