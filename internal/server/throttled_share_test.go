package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucasfd3/Airlink/internal/model"
)

func TestThrottledSharesRecheckExpiryAndRevocationAfterWaiting(t *testing.T) {
	for _, mode := range []string{"expiry", "revocation"} {
		t.Run(mode, func(t *testing.T) {
			s := integrationHost(t)
			original := filepath.Join(t.TempDir(), "private.bin")
			data := bytes.Repeat([]byte("PRIVATE!"), 1024)
			_ = os.WriteFile(original, data, 0600)
			input := model.ShareCreate{Paths: []string{original}}
			if mode == "expiry" {
				input.ExpiresAt = time.Now().Add(200 * time.Millisecond).UTC().Format(time.RFC3339Nano)
			}
			share, err := s.createShare("local", input)
			if err != nil {
				t.Fatal(err)
			}
			secret := share.URL[strings.LastIndex(share.URL, "#")+1:]
			_, token, err := s.shares.Unlock(context.Background(), share.ID, secret, "", "")
			if err != nil {
				t.Fatal(err)
			}
			_ = s.traffic.SetLimits(8192, 0)
			if mode == "revocation" {
				go func() { time.Sleep(100 * time.Millisecond); _ = s.shares.Revoke(share.ID, "local") }()
			}
			req, _ := http.NewRequest(http.MethodGet, hostURL(s)+"/api/v1/public/shares/"+share.ID+"/files/"+share.Files[0].ID, nil)
			req.AddCookie(&http.Cookie{Name: shareCookieName(share.ID), Value: token})
			res, err := pinnedClient(s.fingerprint).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if res.StatusCode != 410 || bytes.Contains(body, []byte("PRIVATE!")) || s.traffic.Snapshot().SentBytes != 0 {
				t.Fatal("throttle released invalid share payload", res.Status, string(body), s.traffic.Snapshot())
			}
		})
	}
}
