package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/lucasfd3/Airlink/internal/model"
	qrcode "github.com/skip2/go-qrcode"
)

type rpcRequest struct {
	ID     any             `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (s *Server) Control(method string, params json.RawMessage) (any, error) {
	if strings.HasPrefix(method, "share.") {
		return s.shareControl(method, params)
	}
	if strings.HasPrefix(method, "clipboard.") {
		return s.clipboardControl(method, params)
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
		return map[string]bool{"ok": true}, s.store.updateDevice(d)
	case "device.revoke":
		if err := s.store.revoke(id.ID); err != nil {
			return nil, err
		}
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
		return map[string]bool{"ok": true}, s.engine.Pause(id.ID)
	case "transfer.resume":
		return map[string]bool{"ok": true}, s.engine.Resume(id.ID)
	case "transfer.cancel":
		s.stopSender(id.ID)
		return map[string]bool{"ok": true}, s.engine.Cancel(id.ID)
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
		var v model.Settings
		if e := json.Unmarshal(params, &v); e != nil {
			return nil, e
		}
		if strings.TrimSpace(v.Name) == "" || len(v.Name) > 120 {
			return nil, errors.New("invalid device name")
		}
		if v.Appearance != "system" && v.Appearance != "light" && v.Appearance != "dark" {
			return nil, errors.New("invalid appearance")
		}
		if e := s.engine.SetReceiveDirectory(v.ReceiveDirectory); e != nil {
			return nil, e
		}
		s.store.Lock()
		s.store.data.Settings = v
		e := s.store.save()
		s.store.Unlock()
		return v, e
	case "shutdown":
		return map[string]bool{"ok": true}, nil
	default:
		return nil, errors.New("unknown control method")
	}
}
func (s *Server) Stdio(in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), 2<<20)
	enc := json.NewEncoder(out)
	for scan.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scan.Bytes(), &req); err != nil {
			_ = enc.Encode(map[string]any{"id": nil, "error": err.Error()})
			continue
		}
		result, err := s.Control(req.Method, req.Params)
		reply := map[string]any{"id": req.ID}
		if err != nil {
			reply["error"] = err.Error()
		} else {
			reply["result"] = result
		}
		if e := enc.Encode(reply); e != nil {
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
</style></head><body>
<header><h1>AirLink</h1><div class="header-controls"><span class="version">0.2.0</span><label><span data-i18n="language">Sprache</span><select id="language"><option value="de">Deutsch</option><option value="en">English</option></select></label></div></header>
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
<section><h2 data-i18n="settings">Einstellungen</h2><form id="settings-form"><div class="form-grid"><label class="field"><span data-i18n="name">Gerätename</span><input id="name" required maxlength="80"></label><label class="field"><span data-i18n="directory">Empfangsordner</span><input id="directory" required></label></div><div class="actions"><button type="submit" data-i18n="save">Speichern</button></div></form></section>
<footer data-i18n="footer">Lokales Netzwerk · Verschlüsselte Übertragungen · Keine Cloud</footer>
<script nonce="NONCE">
history.replaceState(null,'','/');
const el=id=>document.getElementById(id);
const words={de:{language:'Sprache',intro:'Diese lokale Konsole steuert nur diesen Computer. Behalte ihre private Adresse auf diesem Gerät.',connectBrowser:'Browser verbinden',trustHelp:'QR-Code scannen, den Fingerabdruck direkt mit diesem Host vergleichen und sein Zertifikat installieren. Danach die verschlüsselte AirLink-Adresse öffnen.',setup:'Zertifikat einrichten',discover:'Geräte im Netzwerk',pairDesktop:'Desktop koppeln',peerAddress:'Adresse des anderen Computers',peerFingerprint:'Fingerabdruck des anderen Computers',fingerprintPlaceholder:'64 Zeichen aus der AirLink-Anzeige des anderen Computers',verify:'Ich habe den Fingerabdruck direkt in AirLink auf dem anderen Computer verglichen. Angaben aus der Gerätesuche allein bestätigen keine Identität.',requestPair:'Kopplung anfragen',requests:'Kopplungsanfragen',devices:'Vertrauenswürdige Geräte',rightsHelp:'Die Rechte gelten für die Übertragung zwischen diesem Host und dem jeweiligen Gerät.',sendFiles:'Dateien senden',target:'Empfänger',pathsLabel:'Lokale Datei- oder Ordnerpfade · ein vollständiger Pfad pro Zeile',pathsHelp:'Die Pfade müssen auf diesem Computer liegen. Die Originaldateien bleiben unverändert. Andere Geräte können dein Dateisystem nicht durchsuchen.',send:'Senden',transfers:'Übertragungen',settings:'Einstellungen',name:'Gerätename',directory:'Empfangsordner',save:'Speichern',footer:'Lokales Netzwerk · Verschlüsselte Übertragungen · Keine Cloud',noneDiscovered:'Keine weiteren AirLink-Hosts gefunden. Du kannst eine Adresse manuell eingeben.',noneRequests:'Keine offenen Anfragen.',noneDevices:'Noch keine vertrauenswürdigen Geräte.',noneTransfers:'Noch keine Übertragungen.',approve:'Zulassen',deny:'Ablehnen',revoke:'Zugriff entziehen',maySend:'Darf senden',mayReceive:'Darf empfangen',autoAccept:'Automatisch empfangen',useAddress:'Adresse übernehmen',discoveredHelp:'Adresse übernommen. Vergleiche den Fingerabdruck direkt am anderen Computer, bevor du die Kopplung anfragst.',online:'Online',offline:'Offline',pairPending:'Angefragt · am anderen Computer bestätigen',pairComplete:'Gerät erfolgreich gekoppelt.',accept:'Annehmen',pause:'Pausieren',resume:'Fortsetzen',cancel:'Abbrechen',offered:'Wartet auf Annahme',queued:'In der Warteschlange',running:'Wird übertragen',paused:'Pausiert',reconnecting:'Verbindet erneut',verifying:'Wird überprüft',completed:'Abgeschlossen',failed:'Fehlgeschlagen',cancelled:'Abgebrochen',browserReady:'Im Browser zum Download bereit',browserLimit:'Der Browser verwaltet Downloads. Ein erfolgreicher Abschluss auf dem anderen Gerät kann hier nicht bestätigt werden.',to:'An',from:'Von',noTarget:'Zuerst ein Gerät mit Empfangsberechtigung koppeln.',saved:'Einstellungen gespeichert.',pollError:'Verbindung zur lokalen AirLink-Engine unterbrochen. Die Verbindung wird erneut versucht.'},en:{language:'Language',intro:'This local console controls this computer only. Keep its private address on this device.',connectBrowser:'Connect a browser',trustHelp:'Scan the QR code, compare the fingerprint directly with this host, and install its certificate. Then open the encrypted AirLink address.',setup:'Set up certificate',discover:'Devices on your network',pairDesktop:'Pair a desktop',peerAddress:'Other computer’s address',peerFingerprint:'Other computer’s fingerprint',fingerprintPlaceholder:'64 characters shown in AirLink on the other computer',verify:'I compared the fingerprint directly in AirLink on the other computer. Discovery information alone does not verify identity.',requestPair:'Request pairing',requests:'Pairing requests',devices:'Trusted devices',rightsHelp:'These permissions control transfers between this host and each device.',sendFiles:'Send files',target:'Recipient',pathsLabel:'Local file or folder paths · one complete path per line',pathsHelp:'Paths must exist on this computer. Original files stay unchanged. Other devices cannot browse your filesystem.',send:'Send',transfers:'Transfers',settings:'Settings',name:'Device name',directory:'Receive folder',save:'Save',footer:'Local network · Encrypted transfers · No cloud',noneDiscovered:'No additional AirLink hosts found. You can enter an address manually.',noneRequests:'No pending requests.',noneDevices:'No trusted devices yet.',noneTransfers:'No transfers yet.',approve:'Approve',deny:'Decline',revoke:'Revoke access',maySend:'May send',mayReceive:'May receive',autoAccept:'Auto receive',useAddress:'Use address',discoveredHelp:'Address filled in. Compare the fingerprint directly on the other computer before requesting pairing.',online:'Online',offline:'Offline',pairPending:'Requested · approve on the other computer',pairComplete:'Device paired successfully.',accept:'Accept',pause:'Pause',resume:'Resume',cancel:'Cancel',offered:'Waiting for approval',queued:'Queued',running:'Transferring',paused:'Paused',reconnecting:'Reconnecting',verifying:'Verifying',completed:'Complete',failed:'Failed',cancelled:'Cancelled',browserReady:'Ready to download in the browser',browserLimit:'The browser manages downloads. Completion on the other device cannot be verified here.',to:'To',from:'From',noTarget:'First pair a device with receive permission.',saved:'Settings saved.',pollError:'Connection to the local AirLink engine interrupted. Retrying automatically.'}};
Object.assign(words.de,{shares:'Quick Share',shareHelp:'Lokale Dateien per Link im Netzwerk freigeben. Die Originaldateien bleiben an ihrem Ort. Der Host muss eingeschaltet und im lokalen Netzwerk erreichbar bleiben.',sharePaths:'Vollständige lokale Datei- oder Ordnerpfade · ein Pfad pro Zeile',shareName:'Name · optional',sharePassword:'Passwort · optional',shareExpiry:'Ablaufdatum',shareLimit:'Downloadlimit · 0 = unbegrenzt',shareNoExpiry:'Kein Ablaufdatum',shareCreate:'Freigabe erstellen',shareCreated:'Freigabe erstellt.',shareEmpty:'Noch keine Freigaben.',shareCopy:'Link kopieren',shareRevoke:'Widerrufen',shareActive:'Aktiv',shareExpired:'Abgelaufen',shareRevoked:'Widerrufen',shareExhausted:'Downloadlimit erreicht',shareDownloads:'Downloads',shareUnlimited:'unbegrenzt',shareProtected:'Mit Passwort',shareInvalid:'Gib lokale Quellpfade, ein gültiges zukünftiges Ablaufdatum und ein ganzzahliges Downloadlimit ab 0 ein.',copied:'Kopiert.',manualCopy:'Text ausgewählt. Kopiere ihn mit dem Kopierbefehl deines Geräts.',clipboardTitle:'Zwischenablage',clipboardHelp:'Text bewusst senden und kopieren. Kein automatischer Zugriff. Texte bleiben höchstens eine Stunde im laufenden Host und werden nicht dauerhaft gespeichert.',clipboardText:'Text',clipboardPaste:'Einfügen',clipboardSend:'Text senden',clipboardPermission:'Zwischenablage',clipboardNone:'Noch keine Texte.',clipboardNoTarget:'Erlaube zuerst einem vertrauenswürdigen Gerät die Zwischenablage.',clipboardManual:'Füge Text manuell in das Textfeld ein. Der Browser hat keinen Zugriff auf die Zwischenablage erlaubt.',clipboardInvalid:'Text muss zwischen 1 und 65.536 UTF-8-Bytes enthalten.',clipboardSent:'Text gesendet.',clipboardCopy:'Kopieren',clipboardDismiss:'Entfernen'});
Object.assign(words.en,{shares:'Quick Share',shareHelp:'Share local files with a link on your network. Originals stay in place. The host must stay on and reachable on the local network.',sharePaths:'Complete local file or folder paths · one path per line',shareName:'Name · optional',sharePassword:'Password · optional',shareExpiry:'Expiry',shareLimit:'Download limit · 0 = unlimited',shareNoExpiry:'No expiry',shareCreate:'Create share',shareCreated:'Share created.',shareEmpty:'No shares yet.',shareCopy:'Copy link',shareRevoke:'Revoke',shareActive:'Active',shareExpired:'Expired',shareRevoked:'Revoked',shareExhausted:'Download limit reached',shareDownloads:'downloads',shareUnlimited:'unlimited',shareProtected:'Password protected',shareInvalid:'Enter local source paths, a valid future expiry and a whole-number download limit of 0 or more.',copied:'Copied.',manualCopy:'Text selected. Copy it using your device’s copy command.',clipboardTitle:'Clipboard',clipboardHelp:'Send and copy text deliberately. No automatic access. Text stays in the running host for up to one hour and is never saved permanently.',clipboardText:'Text',clipboardPaste:'Paste',clipboardSend:'Send text',clipboardPermission:'Clipboard',clipboardNone:'No text messages yet.',clipboardNoTarget:'First allow Clipboard for a trusted device.',clipboardManual:'Paste text manually into the text field. The browser has not allowed clipboard access.',clipboardInvalid:'Text must contain between 1 and 65,536 UTF-8 bytes.',clipboardSent:'Text sent.',clipboardCopy:'Copy',clipboardDismiss:'Dismiss'});
let language=navigator.language.toLowerCase().startsWith('en')?'en':'de';
try{language=localStorage.getItem('airlink-console-language')||language}catch(e){}
if(!words[language])language='de';
let state,busy=false,requestVersion=0,pendingPeer='';
const t=key=>words[language][key]||key;
function localize(){document.documentElement.lang=language;el('language').value=language;document.querySelectorAll('[data-i18n]').forEach(n=>n.textContent=t(n.dataset.i18n));document.querySelectorAll('[data-placeholder]').forEach(n=>n.placeholder=t(n.dataset.placeholder));if(state)render()}
function node(tag,text,className){const n=document.createElement(tag);if(text!==undefined)n.textContent=text;if(className)n.className=className;return n}
function empty(parent,key){parent.append(node('p',t(key),'empty'))}
function setBusy(value){busy=value;document.querySelectorAll('button,input,textarea,select').forEach(n=>n.disabled=value);syncShareExpiry();syncClipboard()}
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

function render(){
const urls=el('connection');urls.replaceChildren();for(const u of state.urls){const a=node('a',u);a.href=u;a.target='_blank';a.rel='noreferrer noopener';urls.append(a)}el('fingerprint').textContent=state.fingerprint;el('setup').href=((state.setupURLs&&state.setupURLs.length)?state.setupURLs:state.urls.map(x=>x+'/setup'))[0];
const discovered=el('discovered');discovered.replaceChildren();for(const d of state.devices.filter(d=>!d.paired)){row(discovered,d.name||d.id,d.address,[button(t('useAddress'),async()=>fillPeer(d))])}if(!discovered.childNodes.length)empty(discovered,'noneDiscovered');
const requests=el('requests');requests.replaceChildren();for(const p of state.pairRequests){row(requests,p.name,p.kind,[button(t('approve'),()=>call('pair.approve',{id:p.id})),button(t('deny'),()=>call('pair.deny',{id:p.id}))])}if(!requests.childNodes.length)empty(requests,'noneRequests');
const devices=el('devices');devices.replaceChildren();const selected=el('target').value;el('target').replaceChildren();for(const d of state.devices.filter(d=>d.paired)){const main=row(devices,d.name,d.kind+' · '+t(d.online?'online':'offline'),[button(t('revoke'),()=>call('device.revoke',{id:d.id}))]);const rights=node('div',undefined,'rights');rights.append(permission(d,'canSend','maySend'),permission(d,'canReceive','mayReceive'),permission(d,'canClipboard','clipboardPermission'),permission(d,'autoAccept','autoAccept'));main.append(rights);if(d.canReceive){const option=node('option',d.name);option.value=d.id;el('target').append(option)}if(pendingPeer&&d.address===pendingPeer){pendingPeer='';el('pair-progress').textContent=t('pairComplete')}}if(!devices.childNodes.length)empty(devices,'noneDevices');if(selected&&Array.from(el('target').options).some(o=>o.value===selected))el('target').value=selected;
const transfers=el('transfers');transfers.replaceChildren();for(const transfer of state.transfers){const actions=[];const browser=transfer.direction==='send'&&state.devices.some(d=>d.id===transfer.deviceId&&d.kind==='browser');if(transfer.state==='offered'&&transfer.direction==='receive')actions.push(button(t('accept'),()=>call('transfer.accept',{id:transfer.id})));if(['running','queued','reconnecting'].includes(transfer.state))actions.push(button(t('pause'),()=>call('transfer.pause',{id:transfer.id})));if(['paused','reconnecting'].includes(transfer.state))actions.push(button(t('resume'),()=>call('transfer.resume',{id:transfer.id})));if(!['completed','cancelled','failed'].includes(transfer.state))actions.push(button(t('cancel'),()=>call('transfer.cancel',{id:transfer.id})));const main=row(transfers,(transfer.direction==='receive'?t('from'):t('to'))+' '+transfer.deviceName,browser?t('browserReady'):t(transfer.state),actions);if(browser){main.append(node('p',t('browserLimit'),'muted'))}else{const progress=document.createElement('progress');progress.max=100;progress.value=transfer.state==='completed'?100:transfer.totalBytes?Math.min(100,transfer.completedBytes/transfer.totalBytes*100):0;progress.setAttribute('aria-label',t(transfer.state));main.append(progress)}const meta=node('div',undefined,'transfer-meta');meta.append(node('small',bytes(transfer.completedBytes)+' / '+bytes(transfer.totalBytes)),node('small',transfer.bytesPerSecond>0?bytes(transfer.bytesPerSecond)+'/s':''));main.append(meta);if(transfer.error)main.append(node('p',transfer.error,transfer.state==='failed'?'error':'muted'))}if(!transfers.childNodes.length)empty(transfers,'noneTransfers');
renderShares();renderClipboard();
if(document.activeElement!==el('name'))el('name').value=state.settings.name;if(document.activeElement!==el('directory'))el('directory').value=state.settings.receiveDirectory;
}
async function refresh(force=false){if(busy&&!force)return;const version=++requestVersion;const next=await call('status');if(version!==requestVersion)return;state=next;render()}
el('language').addEventListener('change',()=>{language=el('language').value;try{localStorage.setItem('airlink-console-language',language)}catch(e){}localize()});
el('peerurl').addEventListener('input',()=>el('verified').checked=false);el('peerfp').addEventListener('input',()=>el('verified').checked=false);
el('pair-form').addEventListener('submit',e=>{e.preventDefault();if(!el('verified').checked)return;act(async()=>{const url=el('peerurl').value.trim().replace(/\/$/,'');const fingerprint=el('peerfp').value.replace(/[:\s]/g,'').toLowerCase();await call('peer.connect',{url,fingerprint});pendingPeer=url;el('pair-progress').textContent=t('pairPending');el('verified').checked=false})});
el('send-form').addEventListener('submit',e=>{e.preventDefault();act(async()=>{if(!el('target').value)throw Error(t('noTarget'));await call('offer',{deviceId:el('target').value,paths:el('paths').value.split(/\r?\n/).map(p=>p.trim()).filter(Boolean)});el('paths').value=''})});
el('settings-form').addEventListener('submit',e=>{e.preventDefault();act(async()=>{await call('settings.update',{...state.settings,name:el('name').value.trim(),receiveDirectory:el('directory').value.trim()});el('notice').textContent=t('saved')})});

el('share-expiry').value=nextExpiry();
el('share-no-expiry').addEventListener('change',syncShareExpiry);
el('share-form').addEventListener('submit',event=>{event.preventDefault();act(async()=>{const paths=el('share-paths').value.split(/\r?\n/).map(p=>p.trim()).filter(Boolean);const maxDownloads=Number(el('share-limit').value);const expires=el('share-no-expiry').checked?undefined:new Date(el('share-expiry').value);if(!paths.length||!Number.isSafeInteger(maxDownloads)||maxDownloads<0||(expires&&(!Number.isFinite(expires.getTime())||expires.getTime()<=Date.now())))throw Error(t('shareInvalid'));const params={name:el('share-name').value.trim(),paths,maxDownloads};if(el('share-password').value)params.password=el('share-password').value;if(expires)params.expiresAt=expires.toISOString();await call('share.create',params);el('share-password').value='';el('share-name').value='';el('share-expiry').value=nextExpiry();el('notice').textContent=t('shareCreated')})});
el('clipboard-text').addEventListener('input',syncClipboard);el('clipboard-target').addEventListener('change',syncClipboard);
el('clipboard-paste').addEventListener('click',async()=>{try{if(!navigator.clipboard||!navigator.clipboard.readText)throw Error();el('clipboard-text').value=await navigator.clipboard.readText();syncClipboard();el('clipboard-text').focus()}catch(error){el('clipboard-text').focus();el('notice').textContent=t('clipboardManual')}});
el('clipboard-form').addEventListener('submit',event=>{event.preventDefault();act(async()=>{const text=el('clipboard-text').value;const count=new TextEncoder().encode(text).byteLength;if(!el('clipboard-target').value)throw Error(t('clipboardNoTarget'));if(!count||count>65536)throw Error(t('clipboardInvalid'));if(!clipboardPending||clipboardPending.text!==text||clipboardPending.deviceId!==el('clipboard-target').value)clipboardPending={deviceId:el('clipboard-target').value,text};await call('clipboard.send',clipboardPending);clipboardPending=undefined;el('clipboard-text').value='';syncClipboard();el('notice').textContent=t('clipboardSent')})});

localize();refresh().catch(e=>el('error').textContent=e.message);setInterval(()=>refresh().catch(()=>el('error').textContent=t('pollError')),1500);
</script></body></html>`
