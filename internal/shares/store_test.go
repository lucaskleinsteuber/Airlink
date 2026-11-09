package shares

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucaskleinsteuber/Airlink/internal/model"
)

func fixture(t *testing.T) (*Store, string, []Source) {
	t.Helper()
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	sourcePath := filepath.Join(dir, "original.txt")
	if err := os.WriteFile(sourcePath, []byte("untouched source"), 0600); err != nil {
		t.Fatal(err)
	}
	refs, err := CapturePaths([]string{sourcePath})
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, data, refs
}
func fragment(t *testing.T, share model.Share) string {
	t.Helper()
	parts := strings.Split(share.URL, "#")
	if len(parts) != 2 || len(parts[1]) != 64 {
		t.Fatal("invalid recovery URL", share.URL)
	}
	return parts[1]
}
func makeShare(t *testing.T, s *Store, refs []Source, input model.ShareCreate) model.Share {
	t.Helper()
	v, err := s.Create("local", input, refs)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func unlock(t *testing.T, s *Store, v model.Share, password string) string {
	t.Helper()
	got, token, err := s.Unlock(context.Background(), v.ID, fragment(t, v), password, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "" {
		t.Fatal("anonymous response leaked secret URL")
	}
	return token
}

func TestPasswordHashSessionAndRecoverySurviveRestart(t *testing.T) {
	s, data, refs := fixture(t)
	password := "private-passphrase-not-on-disk"
	v := makeShare(t, s, refs, model.ShareCreate{Password: password, MaxDownloads: 2})
	if !v.PasswordProtected {
		t.Fatal("password flag missing")
	}
	if _, _, err := s.Unlock(context.Background(), v.ID, "wrong", password, ""); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("bad secret accepted", err)
	}
	if _, _, err := s.Unlock(context.Background(), v.ID, fragment(t, v), "wrong password", ""); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("bad password accepted", err)
	}
	token := unlock(t, s, v, password)
	if err := s.BeginDownload(v.ID, token); err != nil {
		t.Fatal(err)
	}
	_, same, err := s.Unlock(context.Background(), v.ID, "", "", token)
	if err != nil || same != token {
		t.Fatal("unlock wasn't idempotent", err)
	}
	if err = s.BeginDownload(v.ID, same); err != nil {
		t.Fatal(err)
	}
	r := s.records[v.ID]
	if r.PasswordHash == password || !strings.HasPrefix(r.PasswordHash, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Fatal("password not Argon2id")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shares.sqlite", "shares.sqlite-wal"} {
		b, err := os.ReadFile(filepath.Join(data, name))
		if err == nil && bytes.Contains(b, []byte(password)) {
			t.Fatal("plaintext password persisted")
		}
	}
	s, err = New(data)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.Session(v.ID, token)
	if err != nil || got.Downloads != 1 {
		t.Fatal("session quota lost on restart", got, err)
	}
	owned := s.List("local")
	if len(owned) != 1 || owned[0].URL != v.URL {
		t.Fatal("owner couldn't recover link")
	}
	if len(s.List("different-owner")) != 0 {
		t.Fatal("other owner saw share")
	}
	f, _, err := s.Open(v.ID, refs[0].File.ID)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := io.ReadAll(f)
	f.Close()
	if string(payload) != "untouched source" {
		t.Fatal("source content changed")
	}
}

func TestDownloadQuotaAtomicAcrossConcurrentSessions(t *testing.T) {
	s, _, refs := fixture(t)
	v := makeShare(t, s, refs, model.ShareCreate{MaxDownloads: 1})
	tokens := make([]string, 24)
	for i := range tokens {
		tokens[i] = unlock(t, s, v, "")
	}
	var wg sync.WaitGroup
	results := make(chan string, len(tokens))
	for _, token := range tokens {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			err := s.BeginDownload(v.ID, token)
			if err == nil {
				results <- token
			} else if !errors.Is(err, ErrQuota) {
				t.Errorf("unexpected quota error: %v", err)
			}
		}(token)
	}
	wg.Wait()
	close(results)
	winner := ""
	count := 0
	for token := range results {
		count++
		winner = token
	}
	if count != 1 {
		t.Fatalf("quota allowed %d sessions", count)
	}
	for range 3 {
		if err := s.BeginDownload(v.ID, winner); err != nil {
			t.Fatal("same-session retry consumed quota", err)
		}
	}
	got, _ := s.Get(v.ID)
	if got.Downloads != 1 {
		t.Fatal("quota counter incorrect", got.Downloads)
	}
}

func TestExpiryAndRevocationImmediatelyInvalidatePersistentSession(t *testing.T) {
	s, data, refs := fixture(t)
	v := makeShare(t, s, refs, model.ShareCreate{ExpiresAt: time.Now().Add(150 * time.Millisecond).UTC().Format(time.RFC3339Nano)})
	token := unlock(t, s, v, "")
	time.Sleep(170 * time.Millisecond)
	if err := s.Check(v.ID, token); !errors.Is(err, ErrGone) {
		t.Fatal("expired stream allowed", err)
	}
	other := makeShare(t, s, refs, model.ShareCreate{})
	otherToken := unlock(t, s, other, "")
	if err := s.Revoke(other.ID, "other-owner"); !errors.Is(err, ErrNotFound) {
		t.Fatal("other owner revoked share", err)
	}
	if err := s.Revoke(other.ID, "local"); err != nil {
		t.Fatal(err)
	}
	if err := s.Check(other.ID, otherToken); !errors.Is(err, ErrGone) {
		t.Fatal("revoked stream allowed", err)
	}
	s.Close()
	var err error
	s, err = New(data)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Check(other.ID, otherToken); !errors.Is(err, ErrGone) {
		t.Fatal("revocation lost after restart", err)
	}
}

func TestImmutableReferenceRejectsReplacementAndSymlinkRoots(t *testing.T) {
	s, _, refs := fixture(t)
	v := makeShare(t, s, refs, model.ShareCreate{})
	info, err := os.Stat(refs[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := refs[0].Path + ".replacement"
	if err = os.WriteFile(replacement, []byte("untouched source"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(replacement, refs[0].Path); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Open(v.ID, refs[0].File.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatal("same-size/time inode replacement accepted", err)
	}
	base := t.TempDir()
	root := filepath.Join(base, "received")
	outside := filepath.Join(base, "outside")
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, outside} {
		if err = os.WriteFile(filepath.Join(dir, "file"), []byte("data"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mapping := []Source{{Root: root, Path: filepath.Join(root, "file"), File: model.File{ID: "file", Path: "file", Size: 4}}}
	if _, err = CaptureFiles(mapping); err != nil {
		t.Fatal("valid rooted source rejected", err)
	}
	if err = os.Rename(root, root+"-old"); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(outside, root); err != nil {
		t.Skip(err)
	}
	if _, err = CaptureFiles(mapping); !errors.Is(err, ErrUnavailable) {
		t.Fatal("replaced transfer root escaped confinement", err)
	}
	if _, err = CapturePaths([]string{root}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("selected symlink accepted", err)
	}
}

func TestArgonWorkAndPasswordAttemptsAreBounded(t *testing.T) {
	s, _, refs := fixture(t)
	v := makeShare(t, s, refs, model.ShareCreate{Password: "secret"})
	secret := fragment(t, v)
	s.argonSlots <- struct{}{}
	if _, _, err := s.Unlock(context.Background(), v.ID, secret, "secret", ""); !errors.Is(err, ErrRateLimit) {
		t.Fatal("unbounded parallel Argon2 work", err)
	}
	<-s.argonSlots
	s.mu.Lock()
	for range 5 {
		s.failures[v.ID] = append(s.failures[v.ID], time.Now())
	}
	s.mu.Unlock()
	if _, _, err := s.Unlock(context.Background(), v.ID, secret, "secret", ""); !errors.Is(err, ErrRateLimit) {
		t.Fatal("password attempt throttle bypassed", err)
	}
}

func TestSessionLifetimeCannotBeExtendedByCookieReplay(t *testing.T) {
	s, _, refs := fixture(t)
	v := makeShare(t, s, refs, model.ShareCreate{})
	token := unlock(t, s, v, "")
	s.mu.Lock()
	ss := s.sessions[tokenHash(token)]
	ss.CreatedAt = time.Now().Add(-SessionLifetime - time.Second).UTC().Format(time.RFC3339Nano)
	s.sessions[tokenHash(token)] = ss
	s.mu.Unlock()
	if err := s.Check(v.ID, token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("old session was accepted", err)
	}
	if _, _, err := s.Unlock(context.Background(), v.ID, "", "", token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("old cookie renewed without secret", err)
	}
	_, fresh, err := s.Unlock(context.Background(), v.ID, fragment(t, v), "", token)
	if err != nil || fresh == token {
		t.Fatal("reauthorization didn't issue fresh session", err)
	}
}
