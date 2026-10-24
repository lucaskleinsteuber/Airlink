package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lucasfd3/Airlink/internal/engine"
	"github.com/lucasfd3/Airlink/internal/model"
	"github.com/lucasfd3/Airlink/internal/traffic"
	qrcode "github.com/skip2/go-qrcode"
)

type rpcRequest struct {
	ID     any             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (s *Server) Control(method string, params json.RawMessage) (any, error) {
	if strings.HasPrefix(method, "duplicate.") {
		return s.duplicateControl(method, params)
	}
	if strings.HasPrefix(method, "share.") {
		return s.shareControl(method, params)
	}
	if strings.HasPrefix(method, "clipboard.") {
		return s.clipboardControl(method, params)
	}
	if strings.HasPrefix(method, "relay.") {
		return s.relayControl(method, params)
	}
	if strings.HasPrefix(method, "history.") {
		return s.historyControl(method, params)
	}
	var id struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(params, &id)
	switch method {
	case "status":
		return s.Status(), nil
	case "pair.approve":
		return map[string]bool{"ok": true}, s.approvePair(id.ID, true)
	case "pair.deny":
		return map[string]bool{"ok": true}, s.approvePair(id.ID, false)
	case "device.update":
		var d model.Device
		if e := json.Unmarshal(params, &d); e != nil {
			return nil, e
		}
		if err := s.store.updateDevice(d); err != nil {
			return nil, err
		}
		s.pauseDisallowedTransfers(d)
		if !d.CanRelay {
			s.cancelDeviceRelays(d.ID)
		} else {
			for _, r := range s.relays.List("") {
				if r.SenderID == d.ID && !d.CanSend || r.ReceiverID == d.ID && !d.CanReceive {
					payload, _ := json.Marshal(map[string]string{"id": r.ID})
					_, _ = s.relayControl("relay.cancel", payload)
				}
			}
		}
		return map[string]bool{"ok": true}, nil
	case "device.revoke":
		if err := s.store.revoke(id.ID); err != nil {
			return nil, err
		}
		s.cancelDeviceRelays(id.ID)
		s.dismissDeviceClipboard(id.ID)
		s.cancelDeviceRequests(id.ID)
		for _, t := range s.engine.Transfers(id.ID) {
			if t.State != "completed" && t.State != "cancelled" {
				_ = s.engine.Cancel(t.ID)
			}
		}
		for _, share := range s.shares.List(id.ID) {
			if err := s.shares.Revoke(share.ID, "local"); err != nil {
				return nil, err
			}
		}
		return map[string]bool{"ok": true}, nil
	case "transfer.accept":
		return map[string]bool{"ok": true}, s.engine.Accept(id.ID)
	case "transfer.pause":
		s.stopSender(id.ID)
		err := s.engine.Pause(id.ID)
		s.stopTransferRequests(id.ID)
		return map[string]bool{"ok": true}, err
	case "transfer.resume":
		return map[string]bool{"ok": true}, s.engine.Resume(id.ID)
	case "transfer.cancel":
		s.stopSender(id.ID)
		err := s.engine.Cancel(id.ID)
		s.stopTransferRequests(id.ID)
		return map[string]bool{"ok": true}, err
	case "offer":
		var v struct {
			DeviceID string   `json:"deviceId"`
			Paths    []string `json:"paths"`
		}
		if e := json.Unmarshal(params, &v); e != nil {
			return nil, e
		}
		rec, ok := s.store.snapshot().Devices[v.DeviceID]
		if !ok {
			return nil, errors.New("pair the receiving device first")
		}
		return s.engine.Offer(rec.Device, v.Paths)
	case "peer.connect":
		var v struct {
			URL         string `json:"url"`
			Fingerprint string `json:"fingerprint"`
		}
		if e := json.Unmarshal(params, &v); e != nil {
			return nil, e
		}
		rid, e := s.connectPeer(v.URL, v.Fingerprint)
		return map[string]string{"requestId": rid}, e
	case "settings.update":
		v := s.store.snapshot().Settings
		if e := json.Unmarshal(params, &v); e != nil {
			return nil, e
		}
		if strings.TrimSpace(v.Name) == "" || len(v.Name) > 120 {
			return nil, errors.New("invalid device name")
		}
		if v.Appearance != "system" && v.Appearance != "light" && v.Appearance != "dark" {
			return nil, errors.New("invalid appearance")
		}
		if err := validateShortcut(v.Shortcut); err != nil {
			return nil, err
		}
		if !traffic.ValidLimit(v.SendLimitBytesPerSecond) || !traffic.ValidLimit(v.ReceiveLimitBytesPerSecond) {
			return nil, errors.New("speed limits must be zero or at least1024 bytes/second")
		}
		organization, err := engine.ValidateOrganization(v.Organization)
		if err != nil {
			return nil, err
		}
		v.Organization = organization
		if e := s.engine.SetReceiveDirectory(v.ReceiveDirectory); e != nil {
			return nil, e
		}
		s.store.Lock()
		s.store.data.Settings = v
		e := s.store.save()
		s.store.Unlock()
		if e == nil {
			e = s.traffic.SetLimits(v.SendLimitBytesPerSecond, v.ReceiveLimitBytesPerSecond)
		}
		if e == nil {
			e = s.engine.SetOrganization(v.Organization)
		}
		return v, e
	case "shutdown":
		return map[string]bool{"ok": true}, nil
	default:
		return nil, errors.New("unknown control method")
	}
}
func (s *Server) Stdio(in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(s.ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), 2<<20)
	enc := json.NewEncoder(out)
	var writeMu sync.Mutex
	var outputError error
	write := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		if outputError == nil {
			outputError = enc.Encode(v)
		}
		return outputError
	}
	resolveSlots := make(chan struct{}, 2)
	for scan.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scan.Bytes(), &req); err != nil {
			if e := write(map[string]any{"id": nil, "error": err.Error()}); e != nil {
				return e
			}
			continue
		}
		if req.Method == "duplicate.resolve" {
			select {
			case resolveSlots <- struct{}{}:
				workers.Add(1)
				go func(req rpcRequest) {
					defer workers.Done()
					defer func() { <-resolveSlots }()
					result, err := s.duplicateControlContext(ctx, req.Method, req.Params)
					reply := map[string]any{"id": req.ID}
					if err != nil {
						reply["error"] = err.Error()
					} else {
						reply["result"] = result
					}
					_ = write(reply)
				}(req)
			default:
				if e := write(map[string]any{"id": req.ID, "error": "two duplicate decisions are already being checked"}); e != nil {
					return e
				}
			}
			continue
		}
		result, err := s.Control(req.Method, req.Params)
		reply := map[string]any{"id": req.ID}
		if err != nil {
			reply["error"] = err.Error()
		} else {
			reply["result"] = result
		}
		if e := write(reply); e != nil {
			return e
		}
		if req.Method == "shutdown" && err == nil {
			return nil
		}
	}
	return scan.Err()
}
func (s *Server) startAdmin() error {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return e
	}
	secret := randomID()
	base := "http://" + ln.Addr().String()
	s.adminURL = base + "/?token=" + secret
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if host != ln.Addr().String() {
			http.Error(w, "invalid host", 403)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		cookie, err := r.Cookie("airlink_admin")
		authed := err == nil && cookie.Value == secret
		if r.URL.Query().Get("token") == secret {
			http.SetCookie(w, &http.Cookie{Name: "airlink_admin", Value: secret, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
			authed = true
		}
		if !authed {
			http.Error(w, "open the private setup URL printed by AirLink", 403)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != base {
			http.Error(w, "cross-origin denied", 403)
			return
		}
		if r.URL.Path == "/qr.png" && r.Method == "GET" {
			png, e := qrcode.Encode(s.currentURLs(s.setupURLs)[0], qrcode.Medium, 256)
			if e != nil {
				http.Error(w, "QR unavailable", 500)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
		if r.URL.Path == "/control" && r.Method == "POST" {
			var req rpcRequest
			if !decode(w, r, &req) {
				return
			}
			result, err := s.Control(req.Method, req.Params)
			if err != nil {
				fail(w, 400, err)
				return
			}
			respond(w, 200, result)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self'; script-src 'nonce-"+secret+"'; style-src 'unsafe-inline'; object-src 'none'; frame-ancestors 'none'")
		_, _ = io.WriteString(w, strings.ReplaceAll(adminHTML, "NONCE", secret))
	})
	s.adminServer = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	s.wg.Add(1)
	go func() { defer s.wg.Done(); _ = s.adminServer.Serve(ln) }()
	return nil
}

const adminHTML = `<!doctype html>
<html lang="de"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>AirLink · Local console</title>
<style>
:root{color-scheme:light dark;font:14px -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;--bg:#f5f5f7;--surface:#fff;--text:#1d1d1f;--muted:#6e6e73;--line:#dcdce1;--accent:#0671df;--subtle:#eeeef2}
@media(prefers-color-scheme:dark){:root{--bg:#19191b;--surface:#242427;--text:#f5f5f7;--muted:#a5a5ad;--line:#424247;--accent:#65acff;--subtle:#303034}}
*{box-sizing:border-box}body{max-width:960px;margin:0 auto;padding:30px;color:var(--text);background:var(--bg)}header{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:18px}h1{font-size:27px;letter-spacing:-1px;margin:0}h2{margin:0 0 17px;font-size:18px;letter-spacing:-.3px}h3{font-size:14px;margin:0 0 4px}section{padding:27px 0;border-top:1px solid var(--line)}p{line-height:1.6;margin:7px 0}a{color:var(--accent);text-decoration:none;overflow-wrap:anywhere}button,input,select,textarea{font:inherit;border-radius:8px;border:1px solid var(--line);color:var(--text);background:var(--surface);padding:9px 11px}button{cursor:pointer;font-size:12px;background:var(--subtle);border:0}button.primary{background:#0071e3;color:#fff}button:disabled{opacity:.5;cursor:default}button:focus-visible,a:focus-visible,input:focus-visible,select:focus-visible,textarea:focus-visible{outline:3px solid var(--accent);outline-offset:2px}input[type=checkbox]{accent-color:#0071e3;width:15px;height:15px;margin:0;flex:0 0 auto}input:not([type=checkbox]),textarea{width:100%}textarea{resize:vertical;min-height:90px;line-height:1.55}label{display:flex;align-items:center;gap:7px;font-size:12px}.field{display:grid;gap:7px}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:15px}.full{grid-column:1/-1}.actions,.rights{display:flex;align-items:center;flex-wrap:wrap;gap:9px}.actions{margin-top:12px}.row{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:14px 0;border-bottom:1px solid var(--line)}.row:last-child{border:0}.row-main{min-width:0;flex:1}.muted,small{color:var(--muted);font-size:12px}.notice{border-radius:9px;background:var(--subtle);padding:12px 15px;margin:16px 0;font-size:13px}.notice:empty{display:none}.error{color:#d35038}.connection{display:grid;grid-template-columns:144px 1fr;gap:24px;align-items:start}.qr{width:144px;height:144px;border-radius:12px;display:block;background:#fff;padding:5px}.urls{display:grid;gap:6px;margin-bottom:12px}code{overflow-wrap:anywhere;display:block;color:var(--muted);font:11px ui-monospace,monospace;line-height:1.7;margin-top:9px}.rights{gap:12px;margin-top:10px}.peer-check{margin-top:15px;align-items:flex-start;line-height:1.5}.peer-check input{margin-top:2px}progress{width:100%;height:5px;accent-color:#0071e3;margin-top:10px}.transfer-meta{display:flex;justify-content:space-between;gap:15px;margin-top:5px}.empty{color:var(--muted);font-size:13px;margin:10px 0}.header-controls{display:flex;align-items:center;gap:10px}.version{font-size:11px;color:var(--muted)}.online{color:#248a3d}.paths-help{margin:9px 0 0}#language{width:auto;font-size:12px}footer{padding:20px 0;font-size:11px;color:var(--muted)}@media(max-width:650px){body{padding:22px}.form-grid{grid-template-columns:1fr}.full{grid-column:auto}.connection{grid-template-columns:112px 1fr;gap:15px}.qr{width:112px;height:112px}.row{align-items:flex-start;flex-direction:column;gap:7px}.row .actions{margin-top:4px}.rights{gap:10px}header{gap:12px}.version{display:none}}
.organization-settings{margin-top:25px;border:1px solid var(--line);border-radius:10px;padding:17px}.organization-settings legend{font-size:13px;font-weight:600;padding:0 7px}.organization-rule{border-top:1px solid var(--line);margin-top:15px;padding-top:15px}.organization-rule .actions{margin-top:8px}.duplicate-choice{border-left:2px solid var(--accent);padding-left:15px;margin-top:18px}.duplicate-choice input[readonly]{font-size:11px;margin-top:8px}.duplicate-candidate{margin:12px 0}.skipped-note{color:var(--muted);font-size:12px;margin-top:8px}</style></head><body>
<header><h1>AirLink</h1><div class="header-controls"><span class="version">0.4.0</span><label><span data-i18n="language">Sprache</span><select id="language"><option value="de">Deutsch</option><option value="en">English</option></select></label></div></header>
<p class="muted" data-i18n="intro">Diese lokale Konsole steuert nur diesen Computer. Behalte ihre private Adresse auf diesem Gerät.</p>
<p id="error" class="notice error" role="alert"></p><p id="notice" class="notice" role="status"></p>
<section><h2 data-i18n="connectBrowser">Browser verbinden</h2><div class="connection"><img class="qr" src="/qr.png" width="144" height="144" alt="QR-Code zur lokalen Zertifikatseinrichtung"><div><div id="connection" class="urls"></div><p class="muted" data-i18n="trustHelp">QR-Code scannen, den Fingerabdruck direkt mit diesem Host vergleichen und sein Zertifikat installieren. Danach die verschlüsselte AirLink-Adresse öffnen.</p><a id="setup" target="_blank" rel="noreferrer noopener" data-i18n="setup">Zertifikat einrichten</a><code id="fingerprint"></code></div></div></section>
<section><h2 data-i18n="discover">Geräte im Netzwerk</h2><div id="discovered"></div></section>
<section><h2 data-i18n="pairDesktop">Desktop koppeln</h2><form id="pair-form"><div class="form-grid"><label class="field"><span data-i18n="peerAddress">Adresse des anderen Computers</span><input id="peerurl" placeholder="https://192.168.1.12:43871" type="url" required autocomplete="off"></label><label class="field"><span data-i18n="peerFingerprint">Fingerabdruck des anderen Computers</span><input id="peerfp" data-placeholder="fingerprintPlaceholder" placeholder="64 Zeichen aus der AirLink-Anzeige des anderen Computers" required autocomplete="off"></label></div><label class="peer-check"><input id="verified" type="checkbox" required><span data-i18n="verify">Ich habe den Fingerabdruck direkt in AirLink auf dem anderen Computer verglichen. Angaben aus der Gerätesuche allein bestätigen keine Identität.</span></label><div class="actions"><button id="pair" class="primary" type="submit" data-i18n="requestPair">Kopplung anfragen</button><span id="pair-progress" class="muted" role="status"></span></div></form></section>
<section><h2 data-i18n="requests">Kopplungsanfragen</h2><div id="requests"></div></section>
<section><h2 data-i18n="devices">Vertrauenswürdige Geräte</h2><p class="muted" data-i18n="rightsHelp">Die Rechte gelten für die Übertragung zwischen diesem Host und dem jeweiligen Gerät.</p><div id="devices"></div></section>
<section><h2 data-i18n="sendFiles">Dateien senden</h2><form id="send-form"><div class="form-grid"><label class="field full"><span data-i18n="target">Empfänger</span><select id="target" required></select></label><label class="field full"><span data-i18n="pathsLabel">Lokale Datei- oder Ordnerpfade · ein vollständiger Pfad pro Zeile</span><textarea id="paths" rows="3" required spellcheck="false" autocomplete="off"></textarea></label></div><p class="muted paths-help" data-i18n="pathsHelp">Die Pfade müssen auf diesem Computer liegen. Die Originaldateien bleiben unverändert. Andere Geräte können dein Dateisystem nicht durchsuchen.</p><div class="actions"><button id="send" class="primary" type="submit" data-i18n="send">Senden</button></div></form></section>
<section><h2 data-i18n="transfers">Übertragungen</h2><div id="transfers"></div></section>
<section><h2 data-i18n="shares">Quick Share</h2><p class="muted" data-i18n="shareHelp">Lokale Dateien per Link im Netzwerk freigeben. Die Originaldateien bleiben an ihrem Ort.</p><form id="share-form"><div class="form-grid"><label class="field full"><span data-i18n="sharePaths">Vollständige lokale Datei- oder Ordnerpfade · ein Pfad pro Zeile</span><textarea id="share-paths" rows="3" required spellcheck="false"></textarea></label><label class="field"><span data-i18n="shareName">Name · optional</span><input id="share-name"></label><label class="field"><span data-i18n="sharePassword">Passwort · optional</span><input id="share-password" type="password" autocomplete="new-password"></label><label class="field"><span data-i18n="shareExpiry">Ablaufdatum</span><input id="share-expiry" type="datetime-local" required></label><label class="field"><span data-i18n="shareLimit">Downloadlimit · 0 = unbegrenzt</span><input id="share-limit" type="number" min="0" step="1" value="0" required></label></div><label class="peer-check"><input id="share-no-expiry" type="checkbox"><span data-i18n="shareNoExpiry">Kein Ablaufdatum</span></label><div class="actions"><button class="primary" type="submit" data-i18n="shareCreate">Freigabe erstellen</button></div></form><div id="shares"></div></section>
<section><h2 data-i18n="clipboardTitle">Zwischenablage</h2><p class="muted" data-i18n="clipboardHelp">Text bewusst senden und kopieren. Kein automatischer Zugriff. Texte bleiben höchstens eine Stunde im laufenden Host und werden nicht dauerhaft gespeichert.</p><form id="clipboard-form"><div class="form-grid"><label class="field full"><span data-i18n="target">Empfänger</span><select id="clipboard-target" required></select></label><label class="field full"><span data-i18n="clipboardText">Text</span><textarea id="clipboard-text" rows="4" spellcheck="false" required></textarea></label></div><p id="clipboard-count" class="muted"></p><p id="clipboard-permission" class="muted"></p><div class="actions"><button id="clipboard-paste" type="button" data-i18n="clipboardPaste">Einfügen</button><button id="clipboard-send" class="primary" type="submit" data-i18n="clipboardSend">Text senden</button></div></form><div id="clipboard-messages"></div></section>
<section><h2 data-i18n="relayTitle">Browser-Übertragungen</h2><p class="muted" data-i18n="relayHelp">Beide Browser bleiben geöffnet. Die Daten werden nur im begrenzten Arbeitsspeicher weitergereicht. Jeder Empfänger muss selbst zustimmen; die Konsole nimmt keine Angebote für ihn an.</p><div id="relays"></div></section>
<section><h2 data-i18n="historyTitle">Verlauf</h2><p class="muted" data-i18n="historyHelp">Leeren entfernt nur abgeschlossene, fehlgeschlagene und abgebrochene Metadaten. Dateien, Originale, Freigaben und laufende Übertragungen bleiben erhalten.</p><div class="form-grid"><label class="field"><span data-i18n="historySearch">Datei oder Gerät suchen</span><input id="history-query" type="search"></label><label class="field"><span data-i18n="historyState">Zustand</span><select id="history-state"><option value="" data-i18n="historyAll">Alle</option><option value="offered">offered</option><option value="ready">ready</option><option value="queued">queued</option><option value="running">running</option><option value="paused">paused</option><option value="reconnecting">reconnecting</option><option value="verifying">verifying</option><option value="completed">completed</option><option value="delivered">delivered</option><option value="failed">failed</option><option value="cancelled">cancelled</option></select></label><label class="field"><span data-i18n="historyDirection">Richtung</span><select id="history-direction"><option value="" data-i18n="historyAll">Alle</option><option value="send" data-i18n="historySend">Vom Host gesendet</option><option value="receive" data-i18n="historyReceive">Vom Host empfangen</option><option value="relay" data-i18n="relayTitle">Browser-Übertragungen</option></select></label><label class="field"><span data-i18n="devices">Geräte</span><select id="history-device"><option value="" data-i18n="historyAll">Alle</option></select></label></div><div class="actions"><button id="history-clear" type="button" data-i18n="historyClear">Abgeschlossenen Verlauf leeren</button></div><div id="history-list"></div></section>
<section><h2 data-i18n="statisticsTitle">Statistiken</h2><p class="muted" data-i18n="statisticsHelp">Bestätigte Dateiübertragungen und an Browser ausgelieferte Bytes werden getrennt gezählt. Ein gestarteter Browserdownload bestätigt keine gespeicherte Datei. Lebenslange Traffic-Zähler bleiben beim Leeren des Verlaufs erhalten.</p><div id="statistics"></div><h3 data-i18n="statisticsDaysLabel">Verlauf nach letzter Aktivität (UTC)</h3><div id="statistics-days" style="overflow-x:auto"></div></section>
<section><h2 data-i18n="settings">Einstellungen</h2><form id="settings-form"><div class="form-grid"><label class="field"><span data-i18n="name">Gerätename</span><input id="name" required maxlength="80"></label><label class="field"><span data-i18n="directory">Empfangsordner</span><input id="directory" required></label><label class="field"><span data-i18n="sendLimit">Global senden · KiB/s · 0 = unbegrenzt</span><input id="send-limit" type="number" min="0" max="1073741824" step="any" required></label><label class="field"><span data-i18n="receiveLimit">Global empfangen · KiB/s · 0 = unbegrenzt</span><input id="receive-limit" type="number" min="0" max="1073741824" step="any" required></label></div><fieldset class="organization-settings"><legend data-i18n="organizationTitle">Neue Eingänge organisieren · optional</legend><label class="peer-check"><input id="detect-duplicates" type="checkbox"><span data-i18n="detectDuplicates">Inhaltsgleiche vorhandene Dateien erkennen</span></label><label class="peer-check"><input id="sort-enabled" type="checkbox"><span data-i18n="sortEnabled">Einzelne Dateien nach Endung einsortieren</span></label><p class="muted" data-i18n="organizationHelp">Beides ist standardmäßig aus. Regeln gelten in dieser Reihenfolge nur für neue Eingänge. Übertragene Ordner bleiben vollständig zusammen; bestehende und laufende Dateien werden nicht verschoben.</p><div id="organization-rules"></div><div class="actions"><button id="organization-add" type="button" data-i18n="addRule">Regel hinzufügen</button><button id="organization-presets" type="button" data-i18n="loadPresets">Beispielregeln laden</button></div><p class="muted" data-i18n="presetHelp">Beispiele ändern nur den Entwurf und aktivieren keine Funktion. Prüfe die Ordner und speichere deine Auswahl ausdrücklich.</p></fieldset><p class="muted" data-i18n="limitHelp">Die Limits gelten global für Dateiübertragungen, Freigaben und Browser-Weiterleitungen. 0 bedeutet unbegrenzt.</p><div class="actions"><button type="submit" data-i18n="save">Speichern</button></div></form></section>
<footer data-i18n="footer">Lokales Netzwerk · Verschlüsselte Übertragungen · Keine Cloud</footer>
<script nonce="NONCE">
history.replaceState(null,'','/');
const el=id=>document.getElementById(id);
const words={de:{language:'Sprache',intro:'Diese lokale Konsole steuert nur diesen Computer. Behalte ihre private Adresse auf diesem Gerät.',connectBrowser:'Browser verbinden',trustHelp:'QR-Code scannen, den Fingerabdruck direkt mit diesem Host vergleichen und sein Zertifikat installieren. Danach die verschlüsselte AirLink-Adresse öffnen.',setup:'Zertifikat einrichten',discover:'Geräte im Netzwerk',pairDesktop:'Desktop koppeln',peerAddress:'Adresse des anderen Computers',peerFingerprint:'Fingerabdruck des anderen Computers',fingerprintPlaceholder:'64 Zeichen aus der AirLink-Anzeige des anderen Computers',verify:'Ich habe den Fingerabdruck direkt in AirLink auf dem anderen Computer verglichen. Angaben aus der Gerätesuche allein bestätigen keine Identität.',requestPair:'Kopplung anfragen',requests:'Kopplungsanfragen',devices:'Vertrauenswürdige Geräte',rightsHelp:'Die Rechte gelten für die Übertragung zwischen diesem Host und dem jeweiligen Gerät.',sendFiles:'Dateien senden',target:'Empfänger',pathsLabel:'Lokale Datei- oder Ordnerpfade · ein vollständiger Pfad pro Zeile',pathsHelp:'Die Pfade müssen auf diesem Computer liegen. Die Originaldateien bleiben unverändert. Andere Geräte können dein Dateisystem nicht durchsuchen.',send:'Senden',transfers:'Übertragungen',settings:'Einstellungen',name:'Gerätename',directory:'Empfangsordner',save:'Speichern',footer:'Lokales Netzwerk · Verschlüsselte Übertragungen · Keine Cloud',noneDiscovered:'Keine weiteren AirLink-Hosts gefunden. Du kannst eine Adresse manuell eingeben.',noneRequests:'Keine offenen Anfragen.',noneDevices:'Noch keine vertrauenswürdigen Geräte.',noneTransfers:'Noch keine Übertragungen.',approve:'Zulassen',deny:'Ablehnen',revoke:'Zugriff entziehen',maySend:'Darf senden',mayReceive:'Darf empfangen',autoAccept:'Automatisch empfangen',useAddress:'Adresse übernehmen',discoveredHelp:'Adresse übernommen. Vergleiche den Fingerabdruck direkt am anderen Computer, bevor du die Kopplung anfragst.',online:'Online',offline:'Offline',pairPending:'Angefragt · am anderen Computer bestätigen',pairComplete:'Gerät erfolgreich gekoppelt.',accept:'Annehmen',pause:'Pausieren',resume:'Fortsetzen',cancel:'Abbrechen',offered:'Wartet auf Annahme',queued:'In der Warteschlange',running:'Wird übertragen',paused:'Pausiert',reconnecting:'Verbindet erneut',verifying:'Wird überprüft',completed:'Abgeschlossen',failed:'Fehlgeschlagen',cancelled:'Abgebrochen',browserReady:'Im Browser zum Download bereit',browserLimit:'Der Browser verwaltet Downloads. Ein erfolgreicher Abschluss auf dem anderen Gerät kann hier nicht bestätigt werden.',to:'An',from:'Von',noTarget:'Zuerst ein Gerät mit Empfangsberechtigung koppeln.',saved:'Einstellungen gespeichert.',pollError:'Verbindung zur lokalen AirLink-Engine unterbrochen. Die Verbindung wird erneut versucht.'},en:{language:'Language',intro:'This local console controls this computer only. Keep its private address on this device.',connectBrowser:'Connect a browser',trustHelp:'Scan the QR code, compare the fingerprint directly with this host, and install its certificate. Then open the encrypted AirLink address.',setup:'Set up certificate',discover:'Devices on your network',pairDesktop:'Pair a desktop',peerAddress:'Other computer’s address',peerFingerprint:'Other computer’s fingerprint',fingerprintPlaceholder:'64 characters shown in AirLink on the other computer',verify:'I compared the fingerprint directly in AirLink on the other computer. Discovery information alone does not verify identity.',requestPair:'Request pairing',requests:'Pairing requests',devices:'Trusted devices',rightsHelp:'These permissions control transfers between this host and each device.',sendFiles:'Send files',target:'Recipient',pathsLabel:'Local file or folder paths · one complete path per line',pathsHelp:'Paths must exist on this computer. Original files stay unchanged. Other devices cannot browse your filesystem.',send:'Send',transfers:'Transfers',settings:'Settings',name:'Device name',directory:'Receive folder',save:'Save',footer:'Local network · Encrypted transfers · No cloud',noneDiscovered:'No additional AirLink hosts found. You can enter an address manually.',noneRequests:'No pending requests.',noneDevices:'No trusted devices yet.',noneTransfers:'No transfers yet.',approve:'Approve',deny:'Decline',revoke:'Revoke access',maySend:'May send',mayReceive:'May receive',autoAccept:'Auto receive',useAddress:'Use address',discoveredHelp:'Address filled in. Compare the fingerprint directly on the other computer before requesting pairing.',online:'Online',offline:'Offline',pairPending:'Requested · approve on the other computer',pairComplete:'Device paired successfully.',accept:'Accept',pause:'Pause',resume:'Resume',cancel:'Cancel',offered:'Waiting for approval',queued:'Queued',running:'Transferring',paused:'Paused',reconnecting:'Reconnecting',verifying:'Verifying',completed:'Complete',failed:'Failed',cancelled:'Cancelled',browserReady:'Ready to download in the browser',browserLimit:'The browser manages downloads. Completion on the other device cannot be verified here.',to:'To',from:'From',noTarget:'First pair a device with receive permission.',saved:'Settings saved.',pollError:'Connection to the local AirLink engine interrupted. Retrying automatically.'}};
Object.assign(words.de,{shares:'Quick Share',shareHelp:'Lokale Dateien per Link im Netzwerk freigeben. Die Originaldateien bleiben an ihrem Ort. Der Host muss eingeschaltet und im lokalen Netzwerk erreichbar bleiben.',sharePaths:'Vollständige lokale Datei- oder Ordnerpfade · ein Pfad pro Zeile',shareName:'Name · optional',sharePassword:'Passwort · optional',shareExpiry:'Ablaufdatum',shareLimit:'Downloadlimit · 0 = unbegrenzt',shareNoExpiry:'Kein Ablaufdatum',shareCreate:'Freigabe erstellen',shareCreated:'Freigabe erstellt.',shareEmpty:'Noch keine Freigaben.',shareCopy:'Link kopieren',shareRevoke:'Widerrufen',shareActive:'Aktiv',shareExpired:'Abgelaufen',shareRevoked:'Widerrufen',shareExhausted:'Downloadlimit erreicht',shareDownloads:'Downloads',shareUnlimited:'unbegrenzt',shareProtected:'Mit Passwort',shareInvalid:'Gib lokale Quellpfade, ein gültiges zukünftiges Ablaufdatum und ein ganzzahliges Downloadlimit ab 0 ein.',copied:'Kopiert.',manualCopy:'Text ausgewählt. Kopiere ihn mit dem Kopierbefehl deines Geräts.',clipboardTitle:'Zwischenablage',clipboardHelp:'Text bewusst senden und kopieren. Kein automatischer Zugriff. Texte bleiben höchstens eine Stunde im laufenden Host und werden nicht dauerhaft gespeichert.',clipboardText:'Text',clipboardPaste:'Einfügen',clipboardSend:'Text senden',clipboardPermission:'Zwischenablage',clipboardNone:'Noch keine Texte.',clipboardNoTarget:'Erlaube zuerst einem vertrauenswürdigen Gerät die Zwischenablage.',clipboardManual:'Füge Text manuell in das Textfeld ein. Der Browser hat keinen Zugriff auf die Zwischenablage erlaubt.',clipboardInvalid:'Text muss zwischen 1 und 65.536 UTF-8-Bytes enthalten.',clipboardSent:'Text gesendet.',clipboardCopy:'Kopieren',clipboardDismiss:'Entfernen'});
Object.assign(words.en,{shares:'Quick Share',shareHelp:'Share local files with a link on your network. Originals stay in place. The host must stay on and reachable on the local network.',sharePaths:'Complete local file or folder paths · one path per line',shareName:'Name · optional',sharePassword:'Password · optional',shareExpiry:'Expiry',shareLimit:'Download limit · 0 = unlimited',shareNoExpiry:'No expiry',shareCreate:'Create share',shareCreated:'Share created.',shareEmpty:'No shares yet.',shareCopy:'Copy link',shareRevoke:'Revoke',shareActive:'Active',shareExpired:'Expired',shareRevoked:'Revoked',shareExhausted:'Download limit reached',shareDownloads:'downloads',shareUnlimited:'unlimited',shareProtected:'Password protected',shareInvalid:'Enter local source paths, a valid future expiry and a whole-number download limit of 0 or more.',copied:'Copied.',manualCopy:'Text selected. Copy it using your device’s copy command.',clipboardTitle:'Clipboard',clipboardHelp:'Send and copy text deliberately. No automatic access. Text stays in the running host for up to one hour and is never saved permanently.',clipboardText:'Text',clipboardPaste:'Paste',clipboardSend:'Send text',clipboardPermission:'Clipboard',clipboardNone:'No text messages yet.',clipboardNoTarget:'First allow Clipboard for a trusted device.',clipboardManual:'Paste text manually into the text field. The browser has not allowed clipboard access.',clipboardInvalid:'Text must contain between 1 and 65,536 UTF-8 bytes.',clipboardSent:'Text sent.',clipboardCopy:'Copy',clipboardDismiss:'Dismiss'});
Object.assign(words.de,{relayPermission:'Browser-Weiterleitung',relayTitle:'Browser-Übertragungen',relayHelp:'Beide Browser bleiben geöffnet. Die Daten werden nur im begrenzten Arbeitsspeicher weitergereicht. Jeder Empfänger muss selbst zustimmen; die Konsole nimmt keine Angebote für ihn an.',relayEmpty:'Noch keine Browser-Übertragungen.',relayDelivered:'An Browser ausgeliefert',relayDeliveredHelp:'Ausgeliefert beschreibt den Browsertransport. Nur der Empfängerdownloadmanager bestätigt das Speichern und den Abschluss.',ready:'Zum Laden bereit',delivered:'An Browser ausgeliefert',historyTitle:'Verlauf',historyHelp:'Leeren entfernt nur abgeschlossene, fehlgeschlagene und abgebrochene Metadaten. Dateien, Originale, Freigaben und laufende Übertragungen bleiben erhalten.',historySearch:'Datei oder Gerät suchen',historyState:'Zustand',historyAll:'Alle',historyDirection:'Richtung',historySend:'Vom Host gesendet',historyReceive:'Vom Host empfangen',historyClear:'Abgeschlossenen Verlauf leeren',historyEmpty:'Keine passenden Übertragungen.',historyCleared:'Abgeschlossene Verlaufseinträge entfernt.',statisticsTitle:'Statistiken',statisticsHelp:'Bestätigte Dateiübertragungen und an Browser ausgelieferte Bytes werden getrennt gezählt. Ein gestarteter Browserdownload bestätigt keine gespeicherte Datei. Lebenslange Traffic-Zähler bleiben beim Leeren des Verlaufs erhalten.',statisticsSent:'Bestätigt gesendet',statisticsReceived:'Bestätigt empfangen',statisticsRelay:'Einmalig weitergeleitete Bytes',statisticsDeliveries:'Browser-Auslieferungen',statisticsTrafficSend:'Lebenslanger Payload-Traffic gesendet',statisticsTrafficReceive:'Lebenslanger Payload-Traffic empfangen',statisticsActive:'Aktiv',statisticsCompleted:'Abgeschlossen',statisticsFailed:'Fehlgeschlagen',statisticsCancelled:'Abgebrochen',statisticsDay:'Datum · UTC',statisticsDaysLabel:'Verlauf nach letzter Aktivität (UTC)',sendLimit:'Global senden · KiB/s · 0 = unbegrenzt',receiveLimit:'Global empfangen · KiB/s · 0 = unbegrenzt',limitHelp:'Die Limits gelten global für Dateiübertragungen, Freigaben und Browser-Weiterleitungen. 0 bedeutet unbegrenzt.',invalidLimit:'Ein Limit muss 0 oder zwischen 1 KiB/s und 1 TiB/s liegen.'});
Object.assign(words.en,{relayPermission:'Browser relay',relayTitle:'Browser transfers',relayHelp:'Keep both browsers open. Data is forwarded only through bounded memory. Each recipient must approve; this console never accepts offers for them.',relayEmpty:'No browser transfers yet.',relayDelivered:'Delivered to browser',relayDeliveredHelp:'Delivered describes the browser transport. Only the recipient’s download manager confirms saving and completion.',ready:'Ready to download',delivered:'Delivered to browser',historyTitle:'History',historyHelp:'Clearing removes only completed, failed and cancelled metadata. Files, originals, shares and active transfers stay in place.',historySearch:'Search files or devices',historyState:'State',historyAll:'All',historyDirection:'Direction',historySend:'Sent by host',historyReceive:'Received by host',historyClear:'Clear finished history',historyEmpty:'No matching transfers.',historyCleared:'Finished history records removed.',statisticsTitle:'Statistics',statisticsHelp:'Confirmed file transfers and bytes delivered to browser transports are counted separately. Starting a browser download does not confirm a saved file. Lifetime traffic counters remain when history is cleared.',statisticsSent:'Confirmed sent',statisticsReceived:'Confirmed received',statisticsRelay:'Unique forwarded bytes',statisticsDeliveries:'Browser deliveries',statisticsTrafficSend:'Lifetime payload traffic sent',statisticsTrafficReceive:'Lifetime payload traffic received',statisticsActive:'Active',statisticsCompleted:'Complete',statisticsFailed:'Failed',statisticsCancelled:'Cancelled',statisticsDay:'Date · UTC',statisticsDaysLabel:'History by last activity (UTC)',sendLimit:'Global send · KiB/s · 0 = unlimited',receiveLimit:'Global receive · KiB/s · 0 = unlimited',limitHelp:'Limits apply globally to file transfers, shares and browser forwarding. 0 means unlimited.',invalidLimit:'A limit must be 0 or between 1 KiB/s and 1 TiB/s.'});
Object.assign(words.de,{organizationTitle:'Neue Eingänge organisieren · optional',detectDuplicates:'Inhaltsgleiche vorhandene Dateien erkennen',sortEnabled:'Einzelne Dateien nach Endung einsortieren',organizationHelp:'Beides ist standardmäßig aus. Regeln gelten in dieser Reihenfolge nur für neue Eingänge. Übertragene Ordner bleiben vollständig zusammen; bestehende und laufende Dateien werden nicht verschoben.',sortNeedsRules:'Füge mindestens eine gültige Regel hinzu, bevor du das Einsortieren aktivierst.',addRule:'Regel hinzufügen',loadPresets:'Beispielregeln laden',presetHelp:'Beispiele ändern nur den Entwurf und aktivieren keine Funktion. Prüfe die Ordner und speichere deine Auswahl ausdrücklich.',rule:'Regel',extensions:'Endungen · durch Komma getrennt',relativeFolder:'Relativer Zielordner',moveUp:'Nach oben',moveDown:'Nach unten',removeRule:'Entfernen',invalidRule:'Regeln brauchen eindeutige IDs, gültige Endungen und sichere relative Ordner ohne Traversierung, reservierte Namen oder widersprüchliche Groß-/Kleinschreibung.',checkingDuplicate:'Vorhandene Datei wird überprüft',waitingDuplicate:'Hostentscheidung erforderlich',duplicateHelp:'Inhalt und Größe stimmen mit einer bestehenden verwalteten Datei überein. Du entscheidest ausdrücklich, ob eine neue Kopie empfangen oder der neue Eingang übersprungen wird.',keepDuplicate:'Neue Kopie empfangen',skipDuplicate:'Vorhandene nutzen · Eingang überspringen',showPath:'Vorhandenen Pfad anzeigen',copyPath:'Pfad kopieren',existingCandidate:'Geprüfte vorhandene Datei',skippedFile:'Übersprungen · keine neue Datei',skippedFiles:'Übersprungene Dateien',skippedBytes:'Vermiedene Nutzdaten',skippedHelp:'Vermiedene Daten zählen nicht als neue Dateien oder als Netzwerk-Traffic.',imagesFolder:'Bilder',videosFolder:'Videos',audioFolder:'Audio',documentsFolder:'Dokumente',archivesFolder:'Archive'});
Object.assign(words.en,{organizationTitle:'Organize new incoming files · optional',detectDuplicates:'Detect existing files with identical contents',sortEnabled:'Sort individual files by extension',organizationHelp:'Both are off by default. Rules apply in this order only to new incoming offers. Transferred folders stay together; existing and active files are never moved.',sortNeedsRules:'Add at least one valid rule before enabling sorting.',addRule:'Add rule',loadPresets:'Load example rules',presetHelp:'Examples change only the draft and enable nothing. Review folders and explicitly save your choices.',rule:'Rule',extensions:'Extensions · separated by commas',relativeFolder:'Relative destination folder',moveUp:'Move up',moveDown:'Move down',removeRule:'Remove',invalidRule:'Rules need unique IDs, valid extensions and safe relative folders without traversal, reserved names or conflicting letter case.',checkingDuplicate:'Checking an existing file',waitingDuplicate:'Host decision required',duplicateHelp:'Contents and size match an existing managed file. Explicitly choose to receive a new copy or skip the new incoming file.',keepDuplicate:'Receive a new copy',skipDuplicate:'Use existing · skip incoming file',showPath:'Show existing path',copyPath:'Copy path',existingCandidate:'Verified existing file',skippedFile:'Skipped · no new file',skippedFiles:'Skipped files',skippedBytes:'Avoided payload bytes',skippedHelp:'Avoided bytes do not count as new files or network traffic.',imagesFolder:'Images',videosFolder:'Videos',audioFolder:'Audio',documentsFolder:'Documents',archivesFolder:'Archives'});
let language=navigator.language.toLowerCase().startsWith('en')?'en':'de';
try{language=localStorage.getItem('airlink-console-language')||language}catch(e){}
if(!words[language])language='de';
let state,busy=false,requestVersion=0,pendingPeer='';
const t=key=>words[language][key]||key;
function localize(){document.documentElement.lang=language;el('language').value=language;document.querySelectorAll('[data-i18n]').forEach(n=>n.textContent=t(n.dataset.i18n));document.querySelectorAll('[data-placeholder]').forEach(n=>n.placeholder=t(n.dataset.placeholder));if(state)render()}
function node(tag,text,className){const n=document.createElement(tag);if(text!==undefined)n.textContent=text;if(className)n.className=className;return n}
function empty(parent,key){parent.append(node('p',t(key),'empty'))}
function setBusy(value){busy=value;document.querySelectorAll('button,input,textarea,select').forEach(n=>n.disabled=value);syncShareExpiry();syncClipboard();if(!value&&organizationDraft)renderOrganizationRules()}
async function call(method,params={}){const r=await fetch('/control',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({method,params})});const v=await r.json();if(!r.ok)throw Error(v.error||r.statusText);return v}
async function act(fn){if(busy)return;setBusy(true);el('error').textContent='';try{await fn();await refresh(true)}catch(e){el('error').textContent=e.message}finally{setBusy(false)}}
function button(label,fn){const b=node('button',label);b.type='button';b.disabled=busy;b.addEventListener('click',()=>act(fn));return b}
function row(parent,title,detail,actions=[]){const r=node('div',undefined,'row');const main=node('div',undefined,'row-main');main.append(node('h3',title));if(detail)main.append(node('p',detail,'muted'));r.append(main);if(actions.length){const a=node('div',undefined,'actions');actions.forEach(b=>a.append(b));r.append(a)}parent.append(r);return main}
function bytes(value){if(!value||value<0)return '0 B';const unit=Math.min(4,Math.floor(Math.log(value)/Math.log(1000)));return new Intl.NumberFormat(language,{maximumFractionDigits:unit?1:0}).format(value/Math.pow(1000,unit))+' '+['B','KB','MB','GB','TB'][unit]}
function permission(device,key,label){const wrap=node('label');const input=document.createElement('input');input.type='checkbox';input.checked=!!device[key];input.disabled=busy;input.addEventListener('change',()=>{const checked=input.checked;act(()=>call('device.update',{...state.devices.find(d=>d.id===device.id),[key]:checked}))});wrap.append(input,node('span',t(label)));return wrap}
function fillPeer(device){el('peerurl').value=device.address||'';el('peerfp').value=device.fingerprint||'';el('verified').checked=false;el('notice').textContent=t('discoveredHelp');el('peerurl').focus()}

let shareSignature='',clipboardSignature='',clipboardPending;
function nextExpiry(){const d=new Date(Date.now()+86400000);return new Date(d.getTime()-d.getTimezoneOffset()*60000).toISOString().slice(0,16)}
function syncShareExpiry(){el('share-expiry').disabled=busy||el('share-no-expiry').checked;el('share-expiry').required=!el('share-no-expiry').checked}
function syncClipboard(){const count=new TextEncoder().encode(el('clipboard-text').value).byteLength;el('clipboard-count').textContent=new Intl.NumberFormat(language).format(count)+' / '+new Intl.NumberFormat(language).format(65536)+' B UTF-8';el('clipboard-count').className=count>65536?'error':'muted';el('clipboard-send').disabled=busy||!el('clipboard-target').value||count===0||count>65536;el('clipboard-permission').textContent=el('clipboard-target').options.length?'':t('clipboardNoTarget')}
async function copyExplicit(value,field){try{if(!navigator.clipboard||!navigator.clipboard.writeText)throw Error();await navigator.clipboard.writeText(value);el('notice').textContent=t('copied')}catch(e){field.focus();field.select();el('notice').textContent=t('manualCopy')}}
function renderShares(){const shares=state.shares||[];const signature=language+JSON.stringify(shares)+shares.map(s=>!!(s.expiresAt&&Date.parse(s.expiresAt)<=Date.now())).join(',');if(signature===shareSignature)return;shareSignature=signature;const list=el('shares');list.replaceChildren();for(const share of shares.slice().sort((a,b)=>b.createdAt.localeCompare(a.createdAt))){const expired=share.expiresAt&&Date.parse(share.expiresAt)<=Date.now();const label=share.revoked?'shareRevoked':expired?'shareExpired':share.maxDownloads>0&&share.downloads>=share.maxDownloads?'shareExhausted':'shareActive';const field=document.createElement('input');field.value=share.url||'';field.readOnly=true;field.spellcheck=false;field.setAttribute('aria-label',t('shareCopy'));field.style.fontSize='11px';const actions=[button(t('shareCopy'),()=>copyExplicit(share.url||'',field)),button(t('shareRevoke'),()=>call('share.revoke',{id:share.id}))];actions[0].disabled=busy||!share.url||share.revoked;actions[1].disabled=busy||share.revoked;const main=row(list,share.name,t(label)+' · '+share.downloads+' / '+(share.maxDownloads||t('shareUnlimited'))+' '+t('shareDownloads'),actions);main.append(node('p',bytes(share.totalBytes)+(share.passwordProtected?' · '+t('shareProtected'):'')+' · '+(share.expiresAt?new Date(share.expiresAt).toLocaleString(language):t('shareNoExpiry')),'muted'));main.append(field)}if(!shares.length)empty(list,'shareEmpty')}
function renderClipboard(){const targets=state.devices.filter(d=>d.paired&&d.canClipboard);const select=el('clipboard-target');const targetSignature=JSON.stringify(targets.map(d=>[d.id,d.name]));if(select.dataset.signature!==targetSignature){const selected=select.value;select.replaceChildren();for(const d of targets){const option=node('option',d.name);option.value=d.id;select.append(option)}if(targets.some(d=>d.id===selected))select.value=selected;select.dataset.signature=targetSignature}syncClipboard();const messages=state.clipboard||[];const signature=language+JSON.stringify(messages);if(signature===clipboardSignature)return;clipboardSignature=signature;const list=el('clipboard-messages');list.replaceChildren();for(const message of messages.slice().sort((a,b)=>b.createdAt.localeCompare(a.createdAt))){const field=document.createElement('textarea');field.value=message.text;field.readOnly=true;field.spellcheck=false;field.rows=Math.min(5,Math.max(2,message.text.split('\n').length));field.setAttribute('aria-label',t('clipboardText'));const main=row(list,(message.direction==='receive'?t('from'):t('to'))+' '+message.deviceName,new Date(message.createdAt).toLocaleString(language),[button(t('clipboardCopy'),()=>copyExplicit(message.text,field)),button(t('clipboardDismiss'),()=>call('clipboard.dismiss',{id:message.id}))]);main.append(field)}if(!messages.length)empty(list,'clipboardNone')}


let hostHistory={transfers:[],relays:[]},historyVersion=0,historyTimer,relaySignature='',statisticsSignature='';
function limitBytes(value){if(!String(value).trim())throw Error(t('invalidLimit'));const kib=Number(value);const bps=Math.round(kib*1024);if(!Number.isFinite(kib)||kib<0||!Number.isSafeInteger(bps)||bps>1099511627776||(kib!==0&&bps<1024))throw Error(t('invalidLimit'));return bps}
function historyParams(){const params={limit:100};for(const pair of [['query','history-query'],['state','history-state'],['direction','history-direction'],['deviceId','history-device']])if(el(pair[1]).value)params[pair[0]]=el(pair[1]).value;return params}
async function loadHostHistory(){const version=++historyVersion;try{const next=await call('history.list',historyParams());if(version!==historyVersion)return;hostHistory={transfers:next.transfers||[],relays:next.relays||[]};renderHostHistory()}catch(error){el('error').textContent=error.message}}
function renderHostHistory(){const parent=el('history-list');parent.replaceChildren();const rows=[...hostHistory.transfers.map(value=>({value,relay:false})),...hostHistory.relays.map(value=>({value,relay:true}))].sort((a,b)=>b.value.createdAt.localeCompare(a.value.createdAt));for(const entry of rows){const value=entry.value;const file=value.files.find(f=>!f.directory)||value.files[0];const name=entry.relay?value.senderName+' → '+value.receiverName:value.deviceName;const direction=entry.relay?t('relayTitle'):value.direction==='send'?t('historySend'):t('historyReceive');const main=row(parent,file?file.path:name,name+' · '+direction+' · '+t(value.state));main.append(node('p',new Date(value.createdAt).toLocaleString(language)+' · '+bytes(entry.relay?value.deliveredBytes:value.completedBytes)+' / '+bytes(value.totalBytes),'muted'));if(!entry.relay){if(value.skippedBytes)main.append(node('p',t('skippedBytes')+': '+bytes(value.skippedBytes),'muted'));for(const file of value.files.filter(file=>file.skipped))main.append(node('p',file.path+' · '+t('skippedFile'),'muted'))}if(entry.relay)main.append(node('p',t('relayDeliveredHelp'),'muted'))}if(!rows.length)empty(parent,'historyEmpty')}
function renderHostRelays(){const relays=state.relays||[];const signature=language+JSON.stringify(relays);if(signature===relaySignature)return;relaySignature=signature;const parent=el('relays');parent.replaceChildren();for(const relay of relays){const actions=[];if(['ready','running','queued','reconnecting'].includes(relay.state))actions.push(button(t('pause'),()=>call('relay.pause',{id:relay.id})));if(!['delivered','cancelled','failed'].includes(relay.state))actions.push(button(t('cancel'),()=>call('relay.cancel',{id:relay.id})));const main=row(parent,relay.senderName+' → '+relay.receiverName,t(relay.state),actions);const progress=document.createElement('progress');progress.max=100;progress.value=relay.state==='delivered'?100:relay.totalBytes?relay.deliveredBytes/relay.totalBytes*100:0;progress.setAttribute('aria-label',t('relayTitle'));main.append(progress,node('p',bytes(relay.deliveredBytes)+' / '+bytes(relay.totalBytes)+(relay.bytesPerSecond>0?' · '+bytes(relay.bytesPerSecond)+'/s':''),'muted'),node('p',t('relayDeliveredHelp'),'muted'));if(relay.error)main.append(node('p',relay.error,'error'))}if(!relays.length)empty(parent,'relayEmpty')}
function renderHostStatistics(){const stats=state.statistics;if(!stats)return;const signature=language+JSON.stringify(stats);if(signature===statisticsSignature)return;statisticsSignature=signature;const parent=el('statistics');parent.replaceChildren();const grid=node('div',undefined,'form-grid');for(const pair of [['statisticsSent',bytes(stats.sentBytes)],['statisticsReceived',bytes(stats.receivedBytes)],['statisticsCompleted',stats.completedTransfers],['statisticsFailed',stats.failedTransfers],['statisticsCancelled',stats.cancelledTransfers],['statisticsActive',stats.activeTransfers],['skippedFiles',stats.skippedFiles||0],['skippedBytes',bytes(stats.skippedBytes||0)],['statisticsRelay',bytes(stats.relayedBytes)],['statisticsDeliveries',stats.relayDeliveries],['statisticsTrafficSend',bytes(stats.trafficSentBytes)],['statisticsTrafficReceive',bytes(stats.trafficReceivedBytes)]]){const item=node('div');item.append(node('p',t(pair[0]),'muted'),node('h3',String(pair[1]||0)));grid.append(item)}parent.append(grid);const table=document.createElement('table');table.style.width='100%';table.style.fontSize='12px';table.style.textAlign='left';const head=document.createElement('tr');for(const key of ['statisticsDay','statisticsSent','statisticsReceived','statisticsRelay','statisticsCompleted'])head.append(node('th',t(key)));table.append(head);for(const day of stats.days||[]){const row=document.createElement('tr');for(const value of [day.date,bytes(day.sentBytes),bytes(day.receivedBytes),bytes(day.relayedBytes),String(day.completedTransfers)]){const cell=node('td',value);cell.style.padding='10px 8px 10px 0';row.append(cell)}table.append(row)}el('statistics-days').replaceChildren(table)}
function fillHistoryDevices(){const select=el('history-device');const devices=new Map((state.devices||[]).map(d=>[d.id,d.name]));for(const value of hostHistory.transfers)devices.set(value.deviceId,value.deviceName);for(const value of hostHistory.relays){devices.set(value.senderId,value.senderName);devices.set(value.receiverId,value.receiverName)}const signature=language+JSON.stringify([...devices]);if(select.dataset.signature===signature)return;select.dataset.signature=signature;const selected=select.value;select.replaceChildren();const all=node('option',t('historyAll'));all.value='';select.append(all);for(const [id,name]of devices){const option=node('option',name);option.value=id;select.append(option)}if(devices.has(selected))select.value=selected}


let organizationDraft,organizationDirty=false,organizationLanguage='',candidatePaths=new Map();
function safeOrganizationFolder(value){const folder=String(value);if(new TextEncoder().encode(folder).byteLength>512||!folder||folder.startsWith('/')||/[\\:\p{Cc}<>"|?*]/u.test(folder))throw Error(t('invalidRule'));const parts=folder.split('/');for(const part of parts){const stem=part.split('.')[0].toUpperCase();if(!part||part==='.'||part==='..'||/[. ]$/.test(part)||part.toLowerCase()==='.airlink-partials'||['CON','PRN','AUX','NUL'].includes(stem)||/^(COM|LPT)[1-9]$/.test(stem))throw Error(t('invalidRule'))}return folder}
function normalizedOrganizationRules(rules){if(rules.length>100)throw Error(t('invalidRule'));const ids=new Set(),folders=new Map();return rules.map(rule=>{if(!rule.id||new TextEncoder().encode(rule.id).byteLength>128||ids.has(rule.id))throw Error(t('invalidRule'));ids.add(rule.id);const raw=String(rule.extensionsText||'').split(/[,;\s]+/u).filter(Boolean);if(raw.length>100)throw Error(t('invalidRule'));const extensions=[...new Set(raw.map(ext=>ext.replace(/^\./,'').toLowerCase()))];if(!extensions.length||extensions.some(ext=>ext.length>32||!/^[-_a-z0-9]+$/.test(ext)))throw Error(t('invalidRule'));const folder=safeOrganizationFolder(rule.folder);let path='';for(const part of folder.split('/')){path=path?path+'/'+part:part;const key=path.toLowerCase();if(folders.has(key)&&folders.get(key)!==path)throw Error(t('invalidRule'));folders.set(key,path)}return{id:rule.id,extensions,folder}})}
function organizationSettings(){const rules=normalizedOrganizationRules(organizationDraft?organizationDraft.rules:[]);if(el('sort-enabled').checked&&!rules.length)throw Error(t('sortNeedsRules'));return{detectDuplicates:el('detect-duplicates').checked,sortEnabled:el('sort-enabled').checked,rules}}
function organizationButton(label,fn){const value=node('button',label);value.type='button';value.disabled=busy;value.addEventListener('click',()=>{organizationDirty=true;fn();renderOrganizationRules()});return value}
function renderOrganizationRules(){const parent=el('organization-rules');parent.replaceChildren();organizationLanguage=language;for(const [index,rule]of organizationDraft.rules.entries()){const block=node('div',undefined,'organization-rule');block.append(node('h3',t('rule')+' '+(index+1)));const grid=node('div',undefined,'form-grid');const extensions=node('label',undefined,'field');extensions.append(node('span',t('extensions')));const input=document.createElement('input');input.value=rule.extensionsText;input.required=true;input.spellcheck=false;input.disabled=busy;input.addEventListener('input',()=>{rule.extensionsText=input.value;organizationDirty=true});extensions.append(input);const folder=node('label',undefined,'field');folder.append(node('span',t('relativeFolder')));const field=document.createElement('input');field.value=rule.folder;field.required=true;field.spellcheck=false;field.disabled=busy;field.addEventListener('input',()=>{rule.folder=field.value;organizationDirty=true});folder.append(field);grid.append(extensions,folder);block.append(grid);const actions=node('div',undefined,'actions');const up=organizationButton(t('moveUp'),()=>{if(index>0)[organizationDraft.rules[index-1],organizationDraft.rules[index]]=[organizationDraft.rules[index],organizationDraft.rules[index-1]]});up.disabled=busy||index===0;const down=organizationButton(t('moveDown'),()=>{if(index<organizationDraft.rules.length-1)[organizationDraft.rules[index+1],organizationDraft.rules[index]]=[organizationDraft.rules[index],organizationDraft.rules[index+1]]});down.disabled=busy||index===organizationDraft.rules.length-1;actions.append(up,down,organizationButton(t('removeRule'),()=>organizationDraft.rules.splice(index,1)));block.append(actions);parent.append(block)}}
function syncOrganization(){if(!organizationDirty){const value=state.settings.organization||{detectDuplicates:false,sortEnabled:false,rules:[]};const next={detectDuplicates:!!value.detectDuplicates,sortEnabled:!!value.sortEnabled,rules:(value.rules||[]).map(rule=>({...rule,extensionsText:(rule.extensions||[]).join(', ')}))};if(JSON.stringify(organizationDraft)!==JSON.stringify(next)||organizationLanguage!==language){organizationDraft=next;renderOrganizationRules()}el('detect-duplicates').checked=next.detectDuplicates;el('sort-enabled').checked=next.sortEnabled}else if(organizationLanguage!==language)renderOrganizationRules()}
function organizationPresets(){return[['imagesFolder',['jpg','jpeg','png','gif','heic','webp','tiff']],['videosFolder',['mp4','mov','mkv','avi','m4v','webm']],['audioFolder',['mp3','m4a','aac','wav','flac','ogg']],['documentsFolder',['pdf','doc','docx','txt','md','rtf','xlsx','pptx']],['archivesFolder',['zip','7z','rar','tar','gz']]].map(([name,extensions])=>({id:crypto.randomUUID(),extensionsText:extensions.join(', '),folder:t(name)}))}
async function revealCandidate(candidate,field,copy){const result=candidatePaths.has(candidate.id)?{path:candidatePaths.get(candidate.id)}:await call('duplicate.path',{candidateId:candidate.id});if(!result||typeof result.path!=='string')throw Error(t('invalidRule'));candidatePaths.set(candidate.id,result.path);field.hidden=false;field.value=result.path;if(copy)await copyExplicit(result.path,field)}
function duplicateChoices(parent,transfer){for(const file of transfer.files){if(file.skipped){parent.append(node('p',file.path+' · '+t('skippedFile'),'skipped-note'));continue}if(file.destinationPath&&file.destinationPath!==file.path)parent.append(node('p',file.path+' → '+file.destinationPath,'muted'));if(file.duplicateState!=='pending'&&!(file.duplicateCandidates||[]).length)continue;const choice=node('div',undefined,'duplicate-choice');choice.append(node('h3',file.path),node('p',t('duplicateHelp'),'muted'));if(file.destinationPath&&file.destinationPath!==file.path)choice.append(node('p',file.path+' → '+file.destinationPath,'muted'));const keep=button(t('keepDuplicate'),()=>call('duplicate.resolve',{transferId:transfer.id,fileId:file.id,action:'keep'}));choice.append(keep);for(const candidate of file.duplicateCandidates||[]){const item=node('div',undefined,'duplicate-candidate');item.append(node('p',t('existingCandidate')+' · '+bytes(candidate.size),'muted'));const field=document.createElement('input');field.readOnly=true;field.hidden=!candidatePaths.has(candidate.id);field.value=candidatePaths.get(candidate.id)||'';field.dataset.duplicatePath=candidate.id;field.setAttribute('aria-label',t('existingCandidate'));const actions=node('div',undefined,'actions');for(const [key,copy]of [['showPath',false],['copyPath',true]]){const action=node('button',t(key));action.type='button';action.disabled=busy;action.addEventListener('click',()=>revealCandidate(candidate,field,copy).catch(error=>el('error').textContent=error.message));actions.append(action)}actions.append(button(t('skipDuplicate'),()=>call('duplicate.resolve',{transferId:transfer.id,fileId:file.id,action:'skip',candidateId:candidate.id})));item.append(actions,field);choice.append(item)}parent.append(choice)}}

function render(){
const urls=el('connection');urls.replaceChildren();for(const u of state.urls){const a=node('a',u);a.href=u;a.target='_blank';a.rel='noreferrer noopener';urls.append(a)}el('fingerprint').textContent=state.fingerprint;el('setup').href=((state.setupURLs&&state.setupURLs.length)?state.setupURLs:state.urls.map(x=>x+'/setup'))[0];
const discovered=el('discovered');discovered.replaceChildren();for(const d of state.devices.filter(d=>!d.paired)){row(discovered,d.name||d.id,d.address,[button(t('useAddress'),async()=>fillPeer(d))])}if(!discovered.childNodes.length)empty(discovered,'noneDiscovered');
const requests=el('requests');requests.replaceChildren();for(const p of state.pairRequests){row(requests,p.name,p.kind,[button(t('approve'),()=>call('pair.approve',{id:p.id})),button(t('deny'),()=>call('pair.deny',{id:p.id}))])}if(!requests.childNodes.length)empty(requests,'noneRequests');
const devices=el('devices');devices.replaceChildren();const selected=el('target').value;el('target').replaceChildren();for(const d of state.devices.filter(d=>d.paired)){const main=row(devices,d.name,d.kind+' · '+t(d.online?'online':'offline'),[button(t('revoke'),()=>call('device.revoke',{id:d.id}))]);const rights=node('div',undefined,'rights');rights.append(permission(d,'canSend','maySend'),permission(d,'canReceive','mayReceive'),permission(d,'canClipboard','clipboardPermission'),permission(d,'canRelay','relayPermission'),permission(d,'autoAccept','autoAccept'));main.append(rights);if(d.canReceive){const option=node('option',d.name);option.value=d.id;el('target').append(option)}if(pendingPeer&&d.address===pendingPeer){pendingPeer='';el('pair-progress').textContent=t('pairComplete')}}if(!devices.childNodes.length)empty(devices,'noneDevices');if(selected&&Array.from(el('target').options).some(o=>o.value===selected))el('target').value=selected;
if(!(el('transfers').contains(document.activeElement)&&document.activeElement.dataset&&document.activeElement.dataset.duplicatePath)){const transfers=el('transfers');transfers.replaceChildren();for(const transfer of state.transfers){const actions=[];const browser=transfer.direction==='send'&&state.devices.some(d=>d.id===transfer.deviceId&&d.kind==='browser');if(transfer.state==='offered'&&transfer.direction==='receive')actions.push(button(t('accept'),()=>call('transfer.accept',{id:transfer.id})));if(!browser&&['running','queued','reconnecting','checkingDuplicate','waitingDuplicate'].includes(transfer.state))actions.push(button(t('pause'),()=>call('transfer.pause',{id:transfer.id})));if(!browser&&['paused','reconnecting'].includes(transfer.state))actions.push(button(t('resume'),()=>call('transfer.resume',{id:transfer.id})));if(!['completed','cancelled','failed'].includes(transfer.state))actions.push(button(t('cancel'),()=>call('transfer.cancel',{id:transfer.id})));const main=row(transfers,(transfer.direction==='receive'?t('from'):t('to'))+' '+transfer.deviceName,browser?t('browserReady'):t(transfer.state),actions);if(browser){main.append(node('p',t('browserLimit'),'muted'))}else{const progress=document.createElement('progress');progress.max=100;progress.value=transfer.state==='completed'?100:transfer.totalBytes?Math.min(100,(transfer.completedBytes+(transfer.skippedBytes||0))/transfer.totalBytes*100):0;progress.setAttribute('aria-label',t(transfer.state));main.append(progress)}const meta=node('div',undefined,'transfer-meta');meta.append(node('small',bytes(transfer.completedBytes)+' / '+bytes(transfer.totalBytes)),node('small',transfer.bytesPerSecond>0?bytes(transfer.bytesPerSecond)+'/s':''));main.append(meta);if(transfer.skippedBytes)main.append(node('p',t('skippedBytes')+': '+bytes(transfer.skippedBytes)+' · '+t('skippedHelp'),'muted'));duplicateChoices(main,transfer);if(transfer.error)main.append(node('p',transfer.error,transfer.state==='failed'?'error':'muted'))}if(!transfers.childNodes.length)empty(transfers,'noneTransfers');}
renderShares();renderClipboard();renderHostRelays();renderHostStatistics();renderHostHistory();fillHistoryDevices();syncOrganization();
if(document.activeElement!==el('name'))el('name').value=state.settings.name;if(document.activeElement!==el('directory'))el('directory').value=state.settings.receiveDirectory;if(document.activeElement!==el('send-limit'))el('send-limit').value=(state.settings.sendLimitBytesPerSecond||0)/1024;if(document.activeElement!==el('receive-limit'))el('receive-limit').value=(state.settings.receiveLimitBytesPerSecond||0)/1024;
}
async function refresh(force=false){if(busy&&!force)return;const version=++requestVersion;const next=await call('status');if(version!==requestVersion)return;state=next;render();await loadHostHistory()}
el('language').addEventListener('change',()=>{language=el('language').value;try{localStorage.setItem('airlink-console-language',language)}catch(e){}localize()});
el('peerurl').addEventListener('input',()=>el('verified').checked=false);el('peerfp').addEventListener('input',()=>el('verified').checked=false);
el('pair-form').addEventListener('submit',e=>{e.preventDefault();if(!el('verified').checked)return;act(async()=>{const url=el('peerurl').value.trim().replace(/\/$/,'');const fingerprint=el('peerfp').value.replace(/[:\s]/g,'').toLowerCase();await call('peer.connect',{url,fingerprint});pendingPeer=url;el('pair-progress').textContent=t('pairPending');el('verified').checked=false})});
el('send-form').addEventListener('submit',e=>{e.preventDefault();act(async()=>{if(!el('target').value)throw Error(t('noTarget'));await call('offer',{deviceId:el('target').value,paths:el('paths').value.split(/\r?\n/).map(p=>p.trim()).filter(Boolean)});el('paths').value=''})});
el('settings-form').addEventListener('submit',e=>{e.preventDefault();act(async()=>{await call('settings.update',{...state.settings,name:el('name').value.trim(),receiveDirectory:el('directory').value.trim(),sendLimitBytesPerSecond:limitBytes(el('send-limit').value),receiveLimitBytesPerSecond:limitBytes(el('receive-limit').value),organization:organizationSettings()});organizationDirty=false;el('notice').textContent=t('saved')})});

el('share-expiry').value=nextExpiry();
el('share-no-expiry').addEventListener('change',syncShareExpiry);
el('share-form').addEventListener('submit',event=>{event.preventDefault();act(async()=>{const paths=el('share-paths').value.split(/\r?\n/).map(p=>p.trim()).filter(Boolean);const maxDownloads=Number(el('share-limit').value);const expires=el('share-no-expiry').checked?undefined:new Date(el('share-expiry').value);if(!paths.length||!Number.isSafeInteger(maxDownloads)||maxDownloads<0||(expires&&(!Number.isFinite(expires.getTime())||expires.getTime()<=Date.now())))throw Error(t('shareInvalid'));const params={name:el('share-name').value.trim(),paths,maxDownloads};if(el('share-password').value)params.password=el('share-password').value;if(expires)params.expiresAt=expires.toISOString();await call('share.create',params);el('share-password').value='';el('share-name').value='';el('share-expiry').value=nextExpiry();el('notice').textContent=t('shareCreated')})});
el('clipboard-text').addEventListener('input',syncClipboard);el('clipboard-target').addEventListener('change',syncClipboard);
el('clipboard-paste').addEventListener('click',async()=>{try{if(!navigator.clipboard||!navigator.clipboard.readText)throw Error();el('clipboard-text').value=await navigator.clipboard.readText();syncClipboard();el('clipboard-text').focus()}catch(error){el('clipboard-text').focus();el('notice').textContent=t('clipboardManual')}});
el('clipboard-form').addEventListener('submit',event=>{event.preventDefault();act(async()=>{const text=el('clipboard-text').value;const count=new TextEncoder().encode(text).byteLength;if(!el('clipboard-target').value)throw Error(t('clipboardNoTarget'));if(!count||count>65536)throw Error(t('clipboardInvalid'));if(!clipboardPending||clipboardPending.text!==text||clipboardPending.deviceId!==el('clipboard-target').value)clipboardPending={deviceId:el('clipboard-target').value,text};await call('clipboard.send',clipboardPending);clipboardPending=undefined;el('clipboard-text').value='';syncClipboard();el('notice').textContent=t('clipboardSent')})});


el('history-query').addEventListener('input',()=>{clearTimeout(historyTimer);historyVersion++;historyTimer=setTimeout(()=>loadHostHistory(),250)});for(const id of ['history-state','history-direction','history-device'])el(id).addEventListener('change',()=>loadHostHistory());el('history-clear').addEventListener('click',()=>act(async()=>{await call('history.clear');await loadHostHistory();el('notice').textContent=t('historyCleared')}));


el('detect-duplicates').addEventListener('change',()=>{if(!organizationDraft)organizationDraft={detectDuplicates:false,sortEnabled:false,rules:[]};organizationDraft.detectDuplicates=el('detect-duplicates').checked;organizationDirty=true});el('sort-enabled').addEventListener('change',()=>{if(!organizationDraft)organizationDraft={detectDuplicates:false,sortEnabled:false,rules:[]};organizationDraft.sortEnabled=el('sort-enabled').checked;organizationDirty=true});el('organization-add').addEventListener('click',()=>{if(!organizationDraft)organizationDraft={detectDuplicates:false,sortEnabled:false,rules:[]};organizationDraft.rules.push({id:crypto.randomUUID(),extensionsText:'',folder:''});organizationDirty=true;renderOrganizationRules()});el('organization-presets').addEventListener('click',()=>{if(!organizationDraft)organizationDraft={detectDuplicates:false,sortEnabled:false,rules:[]};organizationDraft.rules=organizationPresets();organizationDirty=true;renderOrganizationRules()});

localize();refresh().catch(e=>el('error').textContent=e.message);setInterval(()=>refresh().catch(()=>el('error').textContent=t('pollError')),1500);
</script></body></html>`
