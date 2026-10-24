package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lucasfd3/Airlink/internal/model"
	"github.com/lucasfd3/Airlink/internal/shares"
)

func sharedSource(t *testing.T, s *Server, data []byte, limit int64) model.Share {
	t.Helper()
	p := filepath.Join(t.TempDir(), "shared-file.txt")
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	v, err := s.createShare("local", model.ShareCreate{Paths: []string{p}, MaxDownloads: limit})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func shareRequest(s *Server, method, p string, cookie *http.Cookie, headers map[string]string, body any) *httptest.ResponseRecorder {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, securityURL(s)+p, bytes.NewReader(b))
	if cookie != nil {
		r.AddCookie(cookie)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, r)
	return w
}
func unlockShared(t *testing.T, s *Server, v model.Share, password string) *http.Cookie {
	t.Helper()
	u, err := url.Parse(v.URL)
	if err != nil {
		t.Fatal(err)
	}
	w := shareRequest(s, "POST", "/api/v1/public/shares/"+v.ID+"/unlock", nil, nil, map[string]string{"secret": u.Fragment, "password": password})
	if w.Code != 200 {
		t.Fatalf("unlock %d: %s", w.Code, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing share cookie")
	}
	c := cookies[0]
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/api/v1/public/shares/"+v.ID {
		t.Fatal("unsafe session cookie", c)
	}
	if c.MaxAge <= 0 || c.Expires.IsZero() {
		t.Fatal("share cookie won't survive browser restart")
	}
	if strings.Contains(w.Body.String(), u.Fragment) || strings.Contains(w.Body.String(), `"url"`) {
		t.Fatal("anonymous unlock leaked recovery URL")
	}
	return c
}
func shareFileURL(v model.Share) string {
	return "/api/v1/public/shares/" + v.ID + "/files/" + v.Files[0].ID
}
func shareCount(t *testing.T, s *Server, v model.Share) int64 {
	t.Helper()
	got, err := s.shares.Get(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got.Downloads
}

func TestSharesRangeAboveTwoGiBAndConditionalRequestsPreserveQuota(t *testing.T) {
	s := newSecurityServer(t)
	p := filepath.Join(t.TempDir(), "large.bin")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	const size int64 = 3*1024*1024*1024 + 7
	if err = f.Truncate(size); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	// This sparse fixture tests 64-bit Range semantics, not a full 3 GiB transfer.
	v, err := s.createShare("local", model.ShareCreate{Paths: []string{p}, MaxDownloads: 1})
	if err != nil {
		t.Fatal(err)
	}
	cookie := unlockShared(t, s, v, "")
	endpoint := shareFileURL(v)
	head := shareRequest(s, "HEAD", endpoint, cookie, nil, nil)
	if head.Code != 200 || head.Header().Get("Content-Length") != "3221225479" {
		t.Fatal("64-bit HEAD failed", head.Code, head.Header())
	}
	if shareCount(t, s, v) != 0 {
		t.Fatal("HEAD consumed allowance")
	}
	bad := shareRequest(s, "GET", endpoint, cookie, map[string]string{"Range": "bytes=4000000000-"}, nil)
	if bad.Code != 416 || shareCount(t, s, v) != 0 {
		t.Fatal("invalid range consumed allowance", bad.Code)
	}
	notModified := shareRequest(s, "GET", endpoint, cookie, map[string]string{"If-None-Match": head.Header().Get("ETag")}, nil)
	if notModified.Code != 304 || shareCount(t, s, v) != 0 {
		t.Fatal("304 consumed allowance", notModified.Code)
	}
	got := shareRequest(s, "GET", endpoint, cookie, map[string]string{"Range": "bytes=3221225472-3221225478"}, nil)
	if got.Code != 206 || got.Body.Len() != 7 || !bytes.Equal(got.Body.Bytes(), make([]byte, 7)) {
		t.Fatal("large range failed", got.Code, got.Body.String())
	}
	if shareCount(t, s, v) != 1 {
		t.Fatal("range not counted")
	}
	retry := shareRequest(s, "GET", endpoint, cookie, map[string]string{"Range": "bytes=0-3"}, nil)
	if retry.Code != 206 || shareCount(t, s, v) != 1 {
		t.Fatal("retry counted twice", retry.Code)
	}
	otherCookie := unlockShared(t, s, v, "")
	blocked := shareRequest(s, "GET", endpoint, otherCookie, map[string]string{"Range": "bytes=0-3"}, nil)
	if blocked.Code != 409 || strings.Contains(blocked.Body.String(), p) {
		t.Fatal("quota or privacy violation", blocked.Code, blocked.Body.String())
	}
}

func TestSharesEmptyFileConsumesOneAllowance(t *testing.T) {
	s := newSecurityServer(t)
	v := sharedSource(t, s, nil, 1)
	cookie := unlockShared(t, s, v, "")
	for _, method := range []string{"GET", "HEAD"} {
		invalid := shareRequest(s, method, shareFileURL(v), cookie, map[string]string{"Range": "bytes=10-20"}, nil)
		if invalid.Code != 416 || invalid.Header().Get("Content-Range") != "bytes */0" || shareCount(t, s, v) != 0 {
			t.Fatal("empty invalid Range consumed quota", method, invalid.Code, invalid.Header(), shareCount(t, s, v))
		}
	}
	for range 2 {
		w := shareRequest(s, "GET", shareFileURL(v), cookie, nil, nil)
		if w.Code != 200 || w.Body.Len() != 0 || shareCount(t, s, v) != 1 {
			t.Fatal("empty download quota incorrect", w.Code, w.Body.String(), shareCount(t, s, v))
		}
	}
	other := unlockShared(t, s, v, "")
	w := shareRequest(s, "GET", shareFileURL(v), other, nil, nil)
	if w.Code != 409 {
		t.Fatal("empty file bypassed quota", w.Code)
	}
}

func TestSharesConcurrentNewSessionsCannotExceedQuota(t *testing.T) {
	s := newSecurityServer(t)
	v := sharedSource(t, s, []byte("one allowed download"), 1)
	cookies := make([]*http.Cookie, 12)
	for i := range cookies {
		cookies[i] = unlockShared(t, s, v, "")
	}
	codes := make(chan int, len(cookies))
	var wg sync.WaitGroup
	for _, cookie := range cookies {
		wg.Add(1)
		go func(c *http.Cookie) {
			defer wg.Done()
			codes <- shareRequest(s, "GET", shareFileURL(v), c, nil, nil).Code
		}(cookie)
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 409 {
			t.Fatal("unexpected response", code)
		}
	}
	if success != 1 || shareCount(t, s, v) != 1 {
		t.Fatal("concurrent quota exceeded", success, shareCount(t, s, v))
	}
}

func TestSharesAnonymousMetadataAndCrossCreatorIsolation(t *testing.T) {
	s := newSecurityServer(t)
	d, tokenA := securityDevice(t, s, "share-owner-a")
	_, tokenB := securityDevice(t, s, "share-owner-b")
	data := []byte("received private content")
	tr, err := s.engine.CreateIncoming(d, []model.File{{ID: "owned-file", Path: "private.txt", Size: int64(len(data)), Modified: 946684800000}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.engine.Accept(tr.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.engine.WriteChunk(context.Background(), tr.ID, "owned-file", 0, digest(data), bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if err = s.engine.CompleteFile(context.Background(), tr.ID, "owned-file", digest(data)); err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(model.ShareCreate{TransferID: tr.ID})
	created := securityRequest(s, "POST", "/api/v1/shares", tokenA, "", input)
	if created.Code != 201 {
		t.Fatalf("completed receive share rejected: %d %s", created.Code, created.Body)
	}
	var v model.Share
	if err = json.Unmarshal(created.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(created.Body.String(), tr.Destination) || v.URL == "" {
		t.Fatal("creator path privacy/recovery violation")
	}
	if w := securityRequest(s, "POST", "/api/v1/shares", tokenB, "", input); w.Code != 404 {
		t.Fatal("other creator shared private transfer", w.Code)
	}
	if w := securityRequest(s, "POST", "/api/v1/shares/"+v.ID+"/revoke", tokenB, "", []byte(`{}`)); w.Code != 404 {
		t.Fatal("other creator revoked private share", w.Code)
	}
	status := s.guestStatus(s.store.snapshot().Devices["share-owner-b"].Device)
	if len(status.Shares) != 0 {
		t.Fatal("guest status leaked another creator share")
	}
	if len(s.shareSummaries(d.ID)) != 1 {
		t.Fatal("owner share missing")
	}
	for _, w := range []*httptest.ResponseRecorder{shareRequest(s, "GET", "/api/v1/public/shares/"+v.ID, nil, nil, nil), shareRequest(s, "POST", "/api/v1/public/shares/"+v.ID+"/unlock", nil, nil, map[string]string{"secret": "wrong"})} {
		if w.Code != 401 || strings.Contains(w.Body.String(), "private.txt") || strings.Contains(w.Body.String(), tr.Destination) {
			t.Fatal("anonymous metadata leak", w.Code, w.Body.String())
		}
	}
	paths, _ := json.Marshal(model.ShareCreate{Paths: []string{tr.Destination}})
	if w := securityRequest(s, "POST", "/api/v1/shares", tokenA, "", paths); w.Code != 400 {
		t.Fatal("browser selected local filesystem paths", w.Code)
	}
	cookie := unlockShared(t, s, v, "")
	if w := shareRequest(s, "GET", shareFileURL(v), cookie, nil, nil); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatal("received share didn't stream original", w.Code)
	}
	params, _ := json.Marshal(map[string]string{"id": d.ID})
	if _, err = s.Control("device.revoke", params); err != nil {
		t.Fatal(err)
	}
	if w := shareRequest(s, "GET", shareFileURL(v), cookie, nil, nil); w.Code != 410 {
		t.Fatal("device revoke left share live", w.Code)
	}
	if _, err = s.createShare(d.ID, model.ShareCreate{TransferID: tr.ID}); err != shares.ErrUnauthorized {
		t.Fatal("revoked creator created an orphan share", err)
	}
	for _, owned := range s.shares.List(d.ID) {
		if !owned.Revoked {
			t.Fatal("orphan share remains active")
		}
	}
}

type revokeOnWrite struct {
	*httptest.ResponseRecorder
	once   sync.Once
	revoke func()
}

func (w *revokeOnWrite) Write(b []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(b)
	w.once.Do(w.revoke)
	return n, err
}
func TestSharesRevocationInterruptsActiveStream(t *testing.T) {
	s := newSecurityServer(t)
	data := bytes.Repeat([]byte("abcdefgh"), 65536)
	v := sharedSource(t, s, data, 0)
	cookie := unlockShared(t, s, v, "")
	r := httptest.NewRequest("GET", securityURL(s)+shareFileURL(v), nil)
	r.AddCookie(cookie)
	var revokeErr error
	w := &revokeOnWrite{ResponseRecorder: httptest.NewRecorder(), revoke: func() { revokeErr = s.shares.Revoke(v.ID, "local") }}
	s.handler().ServeHTTP(w, r)
	if revokeErr != nil {
		t.Fatal(revokeErr)
	}
	if w.Body.Len() == 0 || w.Body.Len() >= len(data) {
		t.Fatal("revocation failed to interrupt body", w.Body.Len(), len(data))
	}
	if next := shareRequest(s, "GET", shareFileURL(v), cookie, nil, nil); next.Code != 410 {
		t.Fatal("old cookie accepted after revoke", next.Code)
	}
}

func TestSharesArchiveUsesSameSessionAndRejectsRange(t *testing.T) {
	s := newSecurityServer(t)
	dir := t.TempDir()
	for name, payload := range map[string]string{"one.txt": "first", "two.txt": "second"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
	}
	v, err := s.createShare("local", model.ShareCreate{Paths: []string{dir}, MaxDownloads: 1})
	if err != nil {
		t.Fatal(err)
	}
	cookie := unlockShared(t, s, v, "")
	endpoint := "/api/v1/public/shares/" + v.ID + "/archive"
	if w := shareRequest(s, "GET", endpoint, cookie, map[string]string{"Range": "bytes=0-1"}, nil); w.Code != 416 || shareCount(t, s, v) != 0 {
		t.Fatal("archive range consumed allowance", w.Code)
	}
	w := shareRequest(s, "GET", endpoint, cookie, nil, nil)
	if w.Code != 200 {
		t.Fatal("archive response", w.Code, w.Body)
	}
	reader, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, f := range reader.File {
		if f.FileInfo().IsDir() {
			continue
		}
		stream, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(stream)
		stream.Close()
		if string(body) != "first" && string(body) != "second" {
			t.Fatal("archive corrupted source")
		}
		files++
	}
	if files != 2 || shareCount(t, s, v) != 1 {
		t.Fatal("archive content/quota incorrect", files)
	}
	for _, f := range v.Files {
		if f.Directory {
			continue
		}
		if next := shareRequest(s, "GET", "/api/v1/public/shares/"+v.ID+"/files/"+f.ID, cookie, nil, nil); next.Code != 200 || shareCount(t, s, v) != 1 {
			t.Fatal("archive and file retry double-counted", next.Code)
		}
		break
	}
}

func TestSharesPasswordSessionScopeAndUnavailableSourceDoNotConsumeQuota(t *testing.T) {
	s := newSecurityServer(t)
	p := filepath.Join(t.TempDir(), "protected-name.txt")
	if err := os.WriteFile(p, []byte("protected source"), 0600); err != nil {
		t.Fatal(err)
	}
	v, err := s.createShare("local", model.ShareCreate{Paths: []string{p}, Password: "correct password", MaxDownloads: 1})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(v.URL)
	wrong := shareRequest(s, "POST", "/api/v1/public/shares/"+v.ID+"/unlock", nil, nil, map[string]string{"secret": u.Fragment, "password": "wrong"})
	if wrong.Code != 401 || strings.Contains(wrong.Body.String(), "protected-name.txt") {
		t.Fatal("wrong password leaked source", wrong.Code)
	}
	cookie := unlockShared(t, s, v, "correct password")
	other := sharedSource(t, s, []byte("other source"), 0)
	forged := *cookie
	forged.Name = shareCookieName(other.ID)
	if w := shareRequest(s, "GET", shareFileURL(other), &forged, nil, nil); w.Code != 401 {
		t.Fatal("share session crossed resource scope", w.Code)
	}
	if err = os.Remove(p); err != nil {
		t.Fatal(err)
	}
	w := shareRequest(s, "GET", shareFileURL(v), cookie, nil, nil)
	if w.Code != 409 || shareCount(t, s, v) != 0 || strings.Contains(w.Body.String(), filepath.Dir(p)) {
		t.Fatal("unavailable source leaked path or consumed quota", w.Code, w.Body.String())
	}
}
