#include "dialogs.h"
#include "core.h"
#include "version.h"
#include "widgets.h"
#include "window.h"
#include <QApplication>
#include <QCheckBox>
#include <QComboBox>
#include <QDateTime>
#include <QDialogButtonBox>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QFormLayout>
#include <QGroupBox>
#include <QHBoxLayout>
#include <QJsonDocument>
#include <QLabel>
#include <QLineEdit>
#include <QMessageBox>
#include <QPushButton>
#include <QScrollArea>
#include <QStandardPaths>
#include <QSysInfo>
#include <QTabWidget>
#include <QTimer>
#include <QVBoxLayout>
#include <memory>

namespace AirLink {
namespace {
QLabel *iconLabel(const QString &name, int size, QWidget *parent) {
  auto *v = new QLabel(parent);
  v->setPixmap(themedIcon(name).pixmap(size, size));
  v->setAlignment(Qt::AlignCenter);
  return v;
}
QString localIconName() {
  QFile chassis("/sys/class/dmi/id/chassis_type");
  if (chassis.open(QIODevice::ReadOnly)) {
    const auto type = chassis.readAll().trimmed();
    if (type == "8" || type == "9" || type == "10" || type == "11" || type == "14" || type == "30" || type == "31" || type == "32")
      return "computer-laptop";
  }
  return "computer";
}
QWidget *step(int number, const QString &value, QWidget *parent) {
  auto *row = new QWidget(parent);
  auto *h = new QHBoxLayout(row);
  h->setContentsMargins(0, 0, 0, 0);
  h->setSpacing(10);
  auto *badge = new QLabel(QString::number(number), row);
  badge->setAlignment(Qt::AlignCenter);
  badge->setFixedSize(24, 24);
  const auto accent = row->palette().color(QPalette::Highlight);
  badge->setStyleSheet(QString("QLabel { background: %1; color: %2; border-radius: 12px; font-weight: 600; }")
                           .arg(accent.name(), row->palette().color(QPalette::HighlightedText).name()));
  h->addWidget(badge, 0, Qt::AlignTop);
  auto *text = styledLabel(value, row, 1.0);
  h->addWidget(text, 1);
  return row;
}
} // namespace

PairingDialog::PairingDialog(const QString &id, QWidget *parent)
    : QDialog(parent), sessionId(id) {
  setAttribute(Qt::WA_DeleteOnClose);
  setWindowTitle("AirLink");
  setMinimumWidth(460);
  auto *v = new QVBoxLayout(this);
  v->setContentsMargins(30, 26, 30, 22);
  v->setSpacing(12);
  auto *icons = new QHBoxLayout;
  icons->addStretch();
  ownIcon = iconLabel(localIconName(), 48, this);
  link = new QLabel(QStringLiteral("• • •"), this);
  link->setAlignment(Qt::AlignCenter);
  link->setMinimumWidth(56);
  peerIcon = iconLabel("computer", 48, this);
  icons->addWidget(ownIcon);
  icons->addWidget(link);
  icons->addWidget(peerIcon);
  icons->addStretch();
  v->addLayout(icons);
  title = styledLabel("", this, 1.35, true);
  title->setAlignment(Qt::AlignCenter);
  v->addWidget(title);
  message = styledLabel("", this, 1.0, false, true);
  message->setAlignment(Qt::AlignCenter);
  v->addWidget(message);
  code = new QLabel(this);
  auto f = code->font();
  f.setPointSizeF(f.pointSizeF() * 2.6);
  f.setWeight(QFont::DemiBold);
  f.setLetterSpacing(QFont::AbsoluteSpacing, 3);
  code->setFont(f);
  code->setAlignment(Qt::AlignCenter);
  code->setTextInteractionFlags(Qt::TextSelectableByMouse);
  v->addWidget(code);
  automatic = new QCheckBox(this);
  automatic->setChecked(true);
  v->addWidget(automatic, 0, Qt::AlignHCenter);
  hint = styledLabel("", this, 0.9, false, true);
  hint->setAlignment(Qt::AlignCenter);
  v->addWidget(hint);
  v->addSpacing(6);
  auto *buttons = new QHBoxLayout;
  buttons->addStretch();
  cancel = new QPushButton(this);
  confirm = new QPushButton(this);
  closeButton = new QPushButton(this);
  confirm->setDefault(true);
  buttons->addWidget(cancel);
  buttons->addWidget(confirm);
  buttons->addWidget(closeButton);
  buttons->addStretch();
  v->addLayout(buttons);
  connect(cancel, &QPushButton::clicked, this, [this] { emit cancelRequested(sessionId); });
  connect(confirm, &QPushButton::clicked, this, [this] {
    confirming = true;
    confirm->setEnabled(false);
    emit confirmRequested(sessionId, automatic->isChecked());
  });
  connect(closeButton, &QPushButton::clicked, this, &QDialog::close);
}

void PairingDialog::refresh(const QJsonObject &s, bool en) {
  const auto tr = [en](const char *de, const char *english) { return QString::fromUtf8(en ? english : de); };
  const auto state = s["state"].toString(), problem = s["problem"].toString();
  const QString name = s["peerName"].toString();
  const QString peer = name.isEmpty() ? tr("dem anderen Computer", "the other computer") : name;
  const QString peerTitle = name.isEmpty() ? tr("Anderer Computer", "Other computer") : name;
  peerIcon->setPixmap(themedIcon(deviceIconName(s["platform"].toString(), s["form"].toString())).pixmap(48, 48));
  const bool comparing = state == "comparing" && !s["localConfirmed"].toBool();
  const bool open = state == "starting" || state == "comparing" || state == "finishing";
  if (!comparing)
    confirming = false;
  code->setVisible(state == "comparing");
  const auto raw = s["comparisonCode"].toString();
  code->setText(raw.size() == 8 ? raw.left(4) + "  " + raw.mid(4) : raw);
  code->setEnabled(comparing);
  automatic->setVisible(comparing);
  automatic->setText(en ? "Accept files from " + peerTitle + " without asking" : "Dateien von " + peerTitle + " ohne Nachfrage annehmen");
  hint->setVisible(comparing);
  hint->setText(tr("Bestätige nur, wenn beide Codes gleich sind. So weißt du sicher, dass du mit deinem eigenen Computer verbunden bist.",
                   "Confirm only if both codes match. That way you know for sure it is your own computer."));
  link->setText(state == "paired" ? QStringLiteral("✓") : QStringLiteral("• • •"));
  if (s.isEmpty()) {
    title->setText(tr("Anfrage abgelaufen", "Request expired"));
    message->setText(tr("Diese Anfrage gibt es nicht mehr. Starte das Verbinden einfach noch einmal.", "This request no longer exists. Simply start connecting again."));
  } else if (state == "paired") {
    title->setText(tr("Verbunden!", "Connected!"));
    message->setText(en ? "Now drag files onto " + peerTitle + ", or click it to choose files."
                        : "Zieh jetzt Dateien auf " + peerTitle + " – oder klick den Computer an, um Dateien auszuwählen.");
  } else if (state == "starting") {
    title->setText((en ? "Connecting to " : "Verbinden mit ") + peerTitle);
    if (problem == "closed")
      message->setText(en ? "Open AirLink on " + peer + ". As soon as it is open there, this continues by itself."
                          : "Öffne AirLink auf " + peer + ". Sobald es dort offen ist, geht es hier von selbst weiter.");
    else if (problem == "unreachable")
      message->setText(en ? peerTitle + " does not answer. Is AirLink open there and are you on the same network? If a firewall is active there, click this computer on " + peer + " instead – that direction works."
                          : peerTitle + " antwortet nicht. Ist AirLink dort geöffnet und seid ihr im selben Netzwerk? Falls dort eine Firewall aktiv ist: Klick stattdessen auf " + peer + " auf diesen Computer – so herum klappt es.");
    else
      message->setText((en ? "Contacting " : "Verbindung zu ") + peer + (en ? " …" : " wird aufgebaut …"));
  } else if (state == "comparing" || state == "finishing") {
    title->setText(s["initiator"].toBool() ? (en ? "Connect to " + peerTitle : "Mit " + peerTitle + " verbinden")
                                           : (en ? peerTitle + " wants to connect" : peerTitle + " möchte sich verbinden"));
    if (comparing)
      message->setText(en ? "Does " + peer + " show the same code?" : "Steht auf " + peer + " derselbe Code?");
    else if (state == "comparing")
      message->setText(en ? "Now confirm on " + peer + " too." : "Bestätige jetzt auch auf " + peer + ".");
    else
      message->setText(tr("Verbindung wird gespeichert …", "Saving the connection …"));
  } else {
    title->setText(tr("Verbinden hat nicht geklappt", "Could not connect"));
    if (problem == "outdated")
      message->setText(en ? peerTitle + " runs an older AirLink. Update AirLink there to 0.13 or later." : peerTitle + " hat eine ältere AirLink-Version. Aktualisiere AirLink dort auf 0.13 oder neuer.");
    else if (problem == "paired")
      message->setText(tr("Ihr seid bereits verbunden.", "You are already connected."));
    else if (problem == "self")
      message->setText(tr("Das ist die Adresse dieses Computers. Gib die Adresse des anderen Computers ein.", "That is the address of this computer. Enter the other computer's address."));
    else if (problem == "identity")
      message->setText(tr("Der Computer hat sich anders ausgewiesen als erwartet. Wähle ihn noch einmal aus der Liste.", "The computer identified itself differently than expected. Choose it from the list again."));
    else if (state == "cancelled" || state == "denied")
      message->setText(tr("Das Verbinden wurde abgebrochen.", "Connecting was cancelled."));
    else
      message->setText(tr("Die Anfrage ist abgelaufen. Versuch es einfach noch einmal.", "The request expired. Simply try again."));
  }
  cancel->setText(comparing && !s["initiator"].toBool() ? tr("Ablehnen", "Decline") : tr("Abbrechen", "Cancel"));
  cancel->setVisible(open && !(s["localConfirmed"].toBool() && state != "comparing"));
  confirm->setText(tr("Code stimmt", "Code Matches"));
  confirm->setVisible(comparing);
  confirm->setEnabled(comparing && !confirming && raw.size() == 8);
  closeButton->setText(state == "paired" ? tr("Fertig", "Done") : tr("Schließen", "Close"));
  closeButton->setVisible(!open);
  closeButton->setDefault(!open);
  adjustSize();
}

AddDeviceDialog::AddDeviceDialog(bool byAddress, bool english, QWidget *parent)
    : QDialog(parent), en(english) {
  setAttribute(Qt::WA_DeleteOnClose);
  setWindowTitle(tr2("Computer hinzufügen", "Add a computer"));
  setMinimumWidth(500);
  auto *v = new QVBoxLayout(this);
  v->setContentsMargins(26, 22, 26, 20);
  v->setSpacing(12);
  v->addWidget(styledLabel(tr2("Computer hinzufügen", "Add a computer"), this, 1.35, true));
  v->addWidget(step(1, tr2("Installiere AirLink auf dem anderen Computer und öffne es.", "Install AirLink on the other computer and open it."), this));
  v->addWidget(step(2, tr2("Beide Computer sind im selben Netzwerk (WLAN oder Kabel).", "Both computers are on the same network (Wi-Fi or cable)."), this));
  v->addWidget(step(3, tr2("Klick den Computer hier an und vergleicht den Code.", "Click the computer here and compare the code."), this));
  v->addSpacing(6);
  v->addWidget(styledLabel(tr2("In der Nähe", "Nearby"), this, 1.05, true));
  auto *card = new Card(this);
  nearbyList = new QVBoxLayout(card);
  nearbyList->setContentsMargins(12, 8, 12, 8);
  nearbyList->setSpacing(6);
  v->addWidget(card);
  auto *addressBox = new QGroupBox(tr2("Nicht gefunden? Per IP-Adresse verbinden", "Not found? Connect by IP address"), this);
  addressBox->setCheckable(true);
  addressBox->setChecked(byAddress);
  auto *av = new QVBoxLayout(addressBox);
  auto *inner = new QWidget(addressBox);
  auto *iv = new QVBoxLayout(inner);
  iv->setContentsMargins(0, 0, 0, 0);
  iv->addWidget(styledLabel(tr2("Gib die IP-Adresse des anderen Computers ein. AirLink zeigt sie dort unter Hilfe.", "Enter the other computer's IP address. AirLink shows it there under Help."), inner, 0.95, false, true));
  auto *row = new QHBoxLayout;
  address = new QLineEdit(inner);
  address->setPlaceholderText(tr2("z. B. 192.168.178.22", "e.g. 192.168.178.22"));
  row->addWidget(address, 1);
  auto *go = button(tr2("Verbinden", "Connect"), inner, [this] {
    const auto value = address->text().trimmed();
    if (value.isEmpty())
      return;
    issue->clear();
    emit connectAddress(value);
  });
  connect(address, &QLineEdit::returnPressed, go, &QPushButton::click);
  row->addWidget(go);
  iv->addLayout(row);
  issue = styledLabel("", inner, 0.95);
  auto pal = issue->palette();
  pal.setColor(QPalette::WindowText, QColor("#D2453C"));
  issue->setPalette(pal);
  iv->addWidget(issue);
  own = styledLabel("", inner, 0.9, false, true);
  own->setTextInteractionFlags(Qt::TextSelectableByMouse);
  iv->addWidget(own);
  av->addWidget(inner);
  inner->setVisible(byAddress);
  connect(addressBox, &QGroupBox::toggled, inner, &QWidget::setVisible);
  v->addWidget(addressBox);
  auto *buttons = new QDialogButtonBox(this);
  buttons->addButton(tr2("Fertig", "Done"), QDialogButtonBox::RejectRole);
  connect(buttons, &QDialogButtonBox::rejected, this, &QDialog::close);
  v->addWidget(buttons);
  if (byAddress)
    address->setFocus();
}
QString AddDeviceDialog::tr2(const char *de, const char *english) const { return QString::fromUtf8(en ? english : de); }
void AddDeviceDialog::setNearby(const QJsonArray &devices) {
  const auto key = QString::fromUtf8(QJsonDocument(devices).toJson(QJsonDocument::Compact));
  if (key == nearbyKey)
    return;
  nearbyKey = key;
  while (auto *item = nearbyList->takeAt(0)) {
    if (auto *w = item->widget()) {
      w->hide();
      w->deleteLater();
    }
    delete item;
  }
  if (devices.isEmpty()) {
    auto *searching = styledLabel(tr2("Suche nach Computern mit AirLink …", "Looking for computers with AirLink …"), this, 1.0, false, true);
    nearbyList->addWidget(searching);
    return;
  }
  for (const auto &v : devices) {
    const auto d = v.toObject();
    auto *row = new QWidget(this);
    auto *h = new QHBoxLayout(row);
    h->setContentsMargins(0, 2, 0, 2);
    h->addWidget(iconLabel(deviceIconName(d["platform"].toString(), d["form"].toString()), 28, row));
    auto *texts = new QVBoxLayout;
    texts->setSpacing(0);
    texts->addWidget(styledLabel(d["name"].toString(), row, 1.0, true));
    if (d["outdated"].toBool())
      texts->addWidget(styledLabel(tr2("Braucht AirLink 0.13 oder neuer", "Needs AirLink 0.13 or later"), row, 0.9, false, true));
    h->addLayout(texts, 1);
    auto *go = button(tr2("Verbinden", "Connect"), row, [this, d] { emit connectDevice(d); });
    go->setEnabled(!d["outdated"].toBool());
    h->addWidget(go);
    nearbyList->addWidget(row);
  }
}
void AddDeviceDialog::setOwnAddress(const QString &value) {
  own->setText(value.isEmpty() ? QString{} : tr2("Die Adresse dieses Computers: ", "This computer's address: ") + value);
}
void AddDeviceDialog::showIssue(const QString &text) { issue->setText(text); }

void MainWindow::sendChooser(const QStringList &paths) {
  auto *dialog = new QDialog(this);
  dialog->setAttribute(Qt::WA_DeleteOnClose);
  dialog->setWindowTitle(text("Wohin senden?", "Send to?"));
  auto *v = new QVBoxLayout(dialog);
  v->setContentsMargins(26, 22, 26, 18);
  auto *title = styledLabel(text("Wohin senden?", "Send to?"), dialog, 1.35, true);
  title->setAlignment(Qt::AlignCenter);
  v->addWidget(title);
  const QString first = QFileInfo(paths.value(0)).fileName();
  auto *summary = styledLabel(paths.size() == 1 ? first : (english() ? QString("%1 and %2 more") : QString("%1 und %2 weitere")).arg(first).arg(paths.size() - 1), dialog, 1.0, false, true);
  summary->setAlignment(Qt::AlignCenter);
  v->addWidget(summary);
  auto *host = new QWidget(dialog);
  auto *flow = new FlowLayout(host, 12);
  const auto status = client.status();
  for (const auto &value : pairedDevices()) {
    const auto d = value.toObject();
    if (!d["canReceive"].toBool())
      continue;
    auto *tile = new DeviceTile(DeviceTile::Kind::Paired, host, 72);
    const auto act = deviceActivity(d, status["transfers"].toArray(), status["pairingSessions"].toArray(), false, english());
    tile->setContent(deviceIconName(d["platform"].toString(), d["form"].toString()), d["name"].toString(), act.label, -1, false, d["online"].toBool());
    const auto id = d["id"].toString();
    connect(tile, &DeviceTile::activated, dialog, [this, dialog, id, paths] {
      dialog->close();
      send(paths, device(id));
    });
    flow->addWidget(tile);
  }
  host->setMinimumWidth(380);
  v->addWidget(host);
  auto *buttons = new QDialogButtonBox(dialog);
  buttons->addButton(text("Abbrechen", "Cancel"), QDialogButtonBox::RejectRole);
  connect(buttons, &QDialogButtonBox::rejected, dialog, &QDialog::close);
  v->addWidget(buttons);
  dialog->show();
}

void MainWindow::deviceSettings(const QString &id) {
  const auto d = device(id);
  if (d.isEmpty())
    return;
  auto *dialog = new QDialog(this);
  dialog->setAttribute(Qt::WA_DeleteOnClose);
  dialog->setWindowTitle(d["name"].toString());
  dialog->setMinimumWidth(440);
  auto *v = new QVBoxLayout(dialog);
  v->setContentsMargins(24, 20, 24, 18);
  auto *head = new QHBoxLayout;
  head->addWidget(iconLabel(deviceIconName(d["platform"].toString(), d["form"].toString()), 40, dialog));
  auto *names = new QVBoxLayout;
  names->addWidget(styledLabel(d["name"].toString(), dialog, 1.3, true));
  names->addWidget(styledLabel(d["online"].toBool() ? text("Verbunden", "Connected") : text("Gerade nicht erreichbar", "Not reachable right now"), dialog, 1.0, false, true));
  head->addLayout(names, 1);
  v->addLayout(head);
  v->addSpacing(8);
  const auto toggle = [this, dialog, v, id](const QString &label, const QString &detail, const QString &key) {
    auto *box = new QCheckBox(label, dialog);
    box->setChecked(device(id)[key].toBool());
    connect(box, &QCheckBox::toggled, this, [this, id, key](bool on) {
      auto current = device(id);
      if (current.isEmpty())
        return;
      current[key] = on;
      updateDevice(current);
    });
    v->addWidget(box);
    if (!detail.isEmpty()) {
      auto *note = styledLabel(detail, dialog, 0.9, false, true);
      note->setContentsMargins(24, 0, 0, 6);
      v->addWidget(note);
    }
  };
  toggle(text("Dateien ohne Nachfrage annehmen", "Accept files without asking"), text("Aus: Du bestätigst jede Sendung einzeln.", "Off: you approve each transfer."), "autoAccept");
  toggle(text("Zwischenablage teilen", "Share clipboard"), text("Kopierten Text per Rechtsklick auf den Computer senden.", "Send copied text by right-clicking the computer."), "canClipboard");
  toggle(text("Darf mir Dateien senden", "May send me files"), {}, "canSend");
  toggle(text("Darf Dateien von mir empfangen", "May receive my files"), {}, "canReceive");
  v->addSpacing(8);
  auto *row = new QHBoxLayout;
  row->addWidget(button(text("Entfernen …", "Remove …"), dialog, [this, dialog, id] {
    removeDevice(device(id));
    if (device(id).isEmpty() || !device(id)["paired"].toBool())
      dialog->close();
  }));
  row->addStretch();
  auto *done = button(text("Fertig", "Done"), dialog, [dialog] { dialog->close(); });
  done->setDefault(true);
  row->addWidget(done);
  v->addLayout(row);
  dialog->show();
}

void MainWindow::openSettings(int tab) {
  if (settingsDialog) {
    if (auto *tabs = settingsDialog->findChild<QTabWidget *>())
      tabs->setCurrentIndex(tab);
    settingsDialog->raise();
    settingsDialog->activateWindow();
    return;
  }
  auto *dialog = new QDialog(this);
  settingsDialog = dialog;
  dialog->setAttribute(Qt::WA_DeleteOnClose);
  dialog->setWindowTitle(text("AirLink-Einstellungen", "AirLink Settings"));
  dialog->resize(560, 520);
  auto *v = new QVBoxLayout(dialog);
  auto *tabs = new QTabWidget(dialog);
  v->addWidget(tabs);
  const auto current = settings();

  auto *general = new QWidget(tabs);
  auto *gf = new QFormLayout(general);
  gf->setContentsMargins(18, 18, 18, 18);
  gf->setRowWrapPolicy(QFormLayout::DontWrapRows);
  auto *name = new QLineEdit(current["name"].toString(), general);
  name->setMaxLength(120);
  connect(name, &QLineEdit::editingFinished, this, [this, name] {
    const auto value = name->text().trimmed();
    if (!value.isEmpty() && value != settings()["name"].toString())
      updateSettings([value](QJsonObject &s) { s["name"] = value; });
  });
  gf->addRow(text("Name dieses Computers", "Name of this computer"), name);
  auto *folderRow = new QHBoxLayout;
  auto *folder = styledLabel(QDir::toNativeSeparators(current["receiveDirectory"].toString()).replace(QDir::homePath(), "~"), general, 1.0, false, true);
  folder->setWordWrap(false);
  folderRow->addWidget(folder, 1);
  folderRow->addWidget(button(text("Ändern …", "Change …"), general, [this, folder] {
    chooseReceiveFolder();
    QTimer::singleShot(600, folder, [this, folder] { folder->setText(QDir::toNativeSeparators(settings()["receiveDirectory"].toString()).replace(QDir::homePath(), "~")); });
  }));
  folderRow->addWidget(button(text("Öffnen", "Open"), general, [this] { openReceiveFolder(); }));
  gf->addRow(text("Empfangene Dateien", "Received files"), folderRow);
  auto *autostart = new QCheckBox(text("Beim Anmelden starten", "Open at login"), general);
  autostart->setChecked(autostartEnabled());
  connect(autostart, &QCheckBox::toggled, this, [this](bool on) { setAutostart(on); });
  gf->addRow(QString{}, autostart);
  auto *notifications = new QCheckBox(text("Mitteilungen", "Notifications"), general);
  notifications->setChecked(current["notifications"].toBool(true));
  connect(notifications, &QCheckBox::toggled, this, [this](bool on) { updateSettings([on](QJsonObject &s) { s["notifications"] = on; }); });
  gf->addRow(QString{}, notifications);
  auto *language = new QComboBox(general);
  language->addItem(text("Wie das System", "System"), "system");
  language->addItem("Deutsch", "de");
  language->addItem("English", "en");
  language->setCurrentIndex(std::max(0, language->findData(current["language"].toString("system"))));
  connect(language, &QComboBox::activated, this, [this, language](int index) {
    const auto value = language->itemData(index).toString();
    updateSettings([value](QJsonObject &s) { s["language"] = value; });
  });
  gf->addRow(text("Sprache", "Language"), language);
  auto *note = styledLabel(text("Nach dem Schließen des Fensters läuft AirLink im Systemabschnitt der Kontrollleiste weiter, damit dich deine Computer erreichen.",
                                "After you close the window, AirLink keeps running in the system tray so your computers can reach you."),
                           general, 0.9, false, true);
  gf->addRow(note);
  tabs->addTab(general, themedIcon("configure"), text("Allgemein", "General"));

  auto *devicesPage = new QScrollArea(tabs);
  devicesPage->setWidgetResizable(true);
  devicesPage->setFrameShape(QFrame::NoFrame);
  auto *devicesBody = new QWidget(devicesPage);
  auto *dv = new QVBoxLayout(devicesBody);
  dv->setContentsMargins(18, 18, 18, 18);
  if (pairedDevices().isEmpty())
    dv->addWidget(styledLabel(text("Noch kein Computer verbunden. Öffne AirLink auf deinem anderen Computer und klick ihn im AirLink-Fenster an.",
                                   "No computer connected yet. Open AirLink on your other computer and click it in the AirLink window."),
                              devicesBody, 1.0, false, true));
  for (const auto &value : pairedDevices()) {
    const auto d = value.toObject();
    const auto id = d["id"].toString();
    auto *box = new QGroupBox(d["name"].toString() + (d["online"].toBool() ? QString{} : text(" · nicht erreichbar", " · not reachable")), devicesBody);
    auto *bv = new QVBoxLayout(box);
    for (const auto &[label, key] : {std::pair{text("Dateien ohne Nachfrage annehmen", "Accept files without asking"), QStringLiteral("autoAccept")},
                                     std::pair{text("Zwischenablage teilen", "Share clipboard"), QStringLiteral("canClipboard")},
                                     std::pair{text("Darf mir Dateien senden", "May send me files"), QStringLiteral("canSend")},
                                     std::pair{text("Darf Dateien von mir empfangen", "May receive my files"), QStringLiteral("canReceive")}}) {
      auto *check = new QCheckBox(label, box);
      check->setChecked(d[key].toBool());
      connect(check, &QCheckBox::toggled, this, [this, id, key](bool on) {
        auto currentDevice = device(id);
        if (currentDevice.isEmpty())
          return;
        currentDevice[key] = on;
        updateDevice(currentDevice);
      });
      bv->addWidget(check);
    }
    bv->addWidget(button(text("Entfernen …", "Remove …"), box, [this, id] { removeDevice(device(id)); }), 0, Qt::AlignLeft);
    dv->addWidget(box);
  }
  dv->addStretch();
  devicesPage->setWidget(devicesBody);
  tabs->addTab(devicesPage, themedIcon("computer"), text("Geräte", "Devices"));

  auto *advanced = new QWidget(tabs);
  auto *af = new QFormLayout(advanced);
  af->setContentsMargins(18, 18, 18, 18);
  const auto limitField = [this, advanced](const QString &key) {
    auto *field = new QLineEdit(advanced);
    const auto value = settings()[key].toDouble();
    field->setText(value > 0 ? QString::number(value / 1e6, 'g', 4) : QString{});
    field->setPlaceholderText(text("unbegrenzt", "unlimited"));
    connect(field, &QLineEdit::editingFinished, this, [this, field, key] {
      bool ok = false;
      const auto raw = field->text().trimmed().replace(',', '.');
      const double mb = raw.toDouble(&ok);
      const qint64 bytesPerSecond = raw.isEmpty() ? 0 : ok && mb > 0 && mb <= 1e6 ? std::max<qint64>(1024, static_cast<qint64>(mb * 1e6)) : -1;
      if (bytesPerSecond < 0)
        return;
      updateSettings([key, bytesPerSecond](QJsonObject &s) { s[key] = bytesPerSecond; });
    });
    auto *row = new QHBoxLayout;
    row->addWidget(field);
    row->addWidget(new QLabel("MB/s", advanced));
    return row;
  };
  af->addRow(text("Senden begrenzen", "Limit sending"), limitField("sendLimitBytesPerSecond"));
  af->addRow(text("Empfangen begrenzen", "Limit receiving"), limitField("receiveLimitBytesPerSecond"));
  auto *updates = new QCheckBox(text("Automatisch nach Updates suchen", "Check for updates automatically"), advanced);
  updates->setChecked(current["automaticUpdateChecks"].toBool());
  connect(updates, &QCheckBox::toggled, this, [this](bool on) { updateSettings([on](QJsonObject &s) { s["automaticUpdateChecks"] = on; }); });
  af->addRow(text("Version ", "Version ") + QString::fromLatin1(Version), updates);
  auto *updateRow = new QHBoxLayout;
  updateRow->addWidget(button(text("Jetzt nach Updates suchen …", "Check Now …"), advanced, [this] { checkUpdates(); }));
  updateLabel = styledLabel(updateMessage, advanced, 0.95, false, true);
  updateRow->addWidget(updateLabel, 1);
  af->addRow(QString{}, updateRow);
  auto *address = styledLabel(localAddresses().join(", "), advanced, 1.0);
  address->setTextInteractionFlags(Qt::TextSelectableByMouse);
  af->addRow(text("Adresse dieses Computers", "This computer's address"), address);
  auto *fingerprint = styledLabel(client.status()["fingerprint"].toString(), advanced, 0.85, false, true);
  fingerprint->setTextInteractionFlags(Qt::TextSelectableByMouse);
  af->addRow(text("Fingerabdruck", "Fingerprint"), fingerprint);
  af->addRow(QString{}, button(text("Diagnosebericht sichern …", "Save Diagnostic Report …"), advanced, [this] { exportDiagnostics(); }));
  tabs->addTab(advanced, themedIcon("preferences-other"), text("Erweitert", "Advanced"));
  tabs->setCurrentIndex(tab);
  auto *buttons = new QDialogButtonBox(dialog);
  buttons->addButton(text("Fertig", "Done"), QDialogButtonBox::RejectRole);
  connect(buttons, &QDialogButtonBox::rejected, dialog, &QDialog::close);
  v->addWidget(buttons);
  dialog->show();
}

void MainWindow::openHistory() {
  if (historyDialog) {
    historyDialog->raise();
    historyDialog->activateWindow();
    return;
  }
  auto *dialog = new QDialog(this);
  historyDialog = dialog;
  dialog->setAttribute(Qt::WA_DeleteOnClose);
  dialog->setWindowTitle(text("Verlauf", "History"));
  dialog->resize(620, 560);
  auto *v = new QVBoxLayout(dialog);
  auto *top = new QHBoxLayout;
  auto *filter = new QComboBox(dialog);
  filter->addItem(text("Alle Computer", "All computers"), QString{});
  for (const auto &value : pairedDevices())
    filter->addItem(value.toObject()["name"].toString(), value.toObject()["id"].toString());
  top->addWidget(filter);
  top->addStretch();
  auto *clear = new QPushButton(text("Verlauf leeren …", "Clear History …"), dialog);
  top->addWidget(clear);
  v->addLayout(top);
  auto *scroll = new QScrollArea(dialog);
  scroll->setWidgetResizable(true);
  auto *body = new QWidget(scroll);
  auto *list = new QVBoxLayout(body);
  list->setContentsMargins(0, 0, 0, 0);
  list->setSpacing(0);
  list->addStretch();
  scroll->setWidget(body);
  v->addWidget(scroll, 1);
  auto *more = new QPushButton(text("Ältere laden", "Load Older"), dialog);
  v->addWidget(more, 0, Qt::AlignHCenter);
  auto cursor = std::make_shared<QString>();
  auto load = std::make_shared<std::function<void(bool)>>();
  QPointer<QDialog> guard(dialog);
  *load = [this, guard, list, body, filter, more, cursor](bool append) {
    QJsonObject query{{"limit", 50}};
    if (append && !cursor->isEmpty())
      query.insert("cursor", *cursor);
    if (!filter->currentData().toString().isEmpty())
      query.insert("deviceId", filter->currentData().toString());
    client.request("history.page", query, [this, guard, list, body, more, cursor, append](QJsonValue value, QString error) {
      if (!guard)
        return;
      if (!error.isEmpty()) {
        issue(error);
        return;
      }
      if (!append)
        while (list->count() > 1) {
          auto *item = list->takeAt(0);
          if (auto *w = item->widget()) {
            w->hide();
            w->deleteLater();
          }
          delete item;
        }
      const auto transfers = value.toObject()["transfers"].toArray();
      for (const auto &item : transfers) {
        auto *row = new TransferRow(body);
        row->setTransfer(item.toObject(), english());
        connect(row, &TransferRow::action, this, &MainWindow::transferAction);
        list->insertWidget(list->count() - 1, row);
      }
      if (list->count() == 1 && transfers.isEmpty())
        list->insertWidget(0, styledLabel(text("Noch keine Übertragungen. Was du sendest und empfängst, erscheint hier.", "No transfers yet. What you send and receive appears here."), body, 1.0, false, true));
      *cursor = value.toObject()["nextCursor"].toString();
      more->setVisible(!cursor->isEmpty());
    });
  };
  connect(more, &QPushButton::clicked, dialog, [load] { (*load)(true); });
  connect(filter, &QComboBox::currentIndexChanged, dialog, [load] { (*load)(false); });
  connect(clear, &QPushButton::clicked, dialog, [this, load] {
    if (QMessageBox::question(this, "AirLink", text("Verlauf leeren? Nur die Liste wird geleert. Deine Dateien bleiben, wo sie sind.",
                                                    "Clear history? Only the list is cleared. Your files stay where they are.")) != QMessageBox::Yes)
      return;
    client.request("history.clear", {}, [this, load](QJsonValue, QString error) {
      if (!error.isEmpty())
        issue(error);
      (*load)(false);
      client.refresh();
    });
  });
  (*load)(false);
  dialog->show();
}

void MainWindow::showHelp() {
  auto *dialog = new QDialog(this);
  dialog->setAttribute(Qt::WA_DeleteOnClose);
  dialog->setWindowTitle(text("AirLink-Hilfe", "AirLink Help"));
  dialog->resize(540, 580);
  auto *outer = new QVBoxLayout(dialog);
  auto *scroll = new QScrollArea(dialog);
  scroll->setWidgetResizable(true);
  scroll->setFrameShape(QFrame::NoFrame);
  auto *body = new QWidget(scroll);
  auto *v = new QVBoxLayout(body);
  v->setContentsMargins(18, 14, 18, 14);
  v->setSpacing(12);
  v->addWidget(styledLabel(text("So funktioniert AirLink", "How AirLink works"), body, 1.35, true));
  v->addWidget(step(1, text("Installiere AirLink auf deinen Computern – Linux, Mac oder Windows.", "Install AirLink on your computers – Linux, Mac or Windows."), body));
  v->addWidget(step(2, text("Verbinde sie einmal: Computer anklicken, Code vergleichen, bestätigen.", "Connect them once: click the computer, compare the code, confirm."), body));
  v->addWidget(step(3, text("Zieh Dateien auf einen Computer. Sie landen dort direkt im Download-Ordner.", "Drag files onto a computer. They arrive directly in its Downloads folder."), body));
  v->addSpacing(6);
  v->addWidget(styledLabel(text("Probleme lösen", "Troubleshooting"), body, 1.1, true));
  const auto help = [body, v](const QString &title, const QString &textValue) {
    auto *box = new QGroupBox(title, body);
    auto *bv = new QVBoxLayout(box);
    auto *content = styledLabel(textValue, box, 0.95, false, true);
    content->setTextInteractionFlags(Qt::TextSelectableByMouse);
    bv->addWidget(content);
    v->addWidget(box);
  };
  help(text("Der andere Computer erscheint nicht", "The other computer does not appear"),
       text("AirLink muss auf beiden Computern geöffnet sein, und beide müssen im selben Netzwerk sein. Gastnetze und manche Router trennen Geräte voneinander – dann hilft „Per IP-Adresse verbinden“ unter „Hinzufügen“.",
            "AirLink must be open on both computers, and both must be on the same network. Guest networks and some routers keep devices apart – then use “Connect by IP address” under “Add”."));
  help(text("Verbinden klappt nicht (Firewall)", "Connecting does not work (firewall)"),
       text("Ist auf diesem Computer eine Firewall aktiv (ufw, firewalld), starte das Verbinden hier – so herum klappt es, und danach funktioniert das Senden in beide Richtungen. Oder gib AirLink frei: sudo ufw allow 43871/tcp (bzw. sudo firewall-cmd --permanent --add-service=airlink).",
            "If a firewall is active on this computer (ufw, firewalld), start connecting from here – that direction works, and afterwards sending works both ways. Or allow AirLink: sudo ufw allow 43871/tcp (or sudo firewall-cmd --permanent --add-service=airlink)."));
  help(text("Eine Übertragung hängt", "A transfer is stuck"),
       text("AirLink macht automatisch weiter, sobald beide Computer wieder verbunden sind – auch nach einem Neustart. Was schon angekommen ist, wird nicht noch einmal gesendet.",
            "AirLink continues automatically as soon as both computers are connected again – even after a restart. What already arrived is not sent again."));
  help(text("Wo landen meine Dateien?", "Where do my files go?"),
       text("Im Empfangsordner, normalerweise „Downloads“. Ändern kannst du ihn in den Einstellungen.", "In the receive folder, usually Downloads. You can change it in Settings."));
  help(text("Senden aus dem Dateimanager", "Sending from the file manager"),
       text("In Dolphin: Rechtsklick auf Dateien → „Mit AirLink senden“.", "In Dolphin: right-click files → “Send with AirLink”."));
  if (const auto own = localAddresses().value(0); !own.isEmpty()) {
    auto *address = styledLabel(text("Adresse dieses Computers: ", "This computer's address: ") + own, body, 1.0, true);
    address->setTextInteractionFlags(Qt::TextSelectableByMouse);
    v->addWidget(address);
  }
  v->addStretch();
  scroll->setWidget(body);
  outer->addWidget(scroll);
  auto *row = new QHBoxLayout;
  row->addWidget(button(text("Empfangsordner öffnen", "Open Receive Folder"), dialog, [this] { openReceiveFolder(); }));
  row->addStretch();
  auto *done = button(text("Fertig", "Done"), dialog, [dialog] { dialog->close(); });
  done->setDefault(true);
  row->addWidget(done);
  outer->addLayout(row);
  dialog->show();
}

namespace {
QJsonObject demoStatus(const QString &scenario, bool en) {
  const auto now = QDateTime::currentDateTimeUtc();
  const auto ago = [&now](int seconds) { return now.addSecs(-seconds).toString(Qt::ISODateWithMs); };
  QJsonArray devices, transfers, pairings, clipboard;
  const QJsonObject linux_{{"id", "demo-linux"}, {"name", "MacBook Pro"}, {"kind", "engine"}, {"address", "https://192.0.2.10:43871"}, {"fingerprint", "aa"}, {"paired", true}, {"online", true}, {"canSend", true}, {"canReceive", true}, {"canClipboard", true}, {"autoAccept", true}, {"platform", "macos"}, {"form", "laptop"}};
  const QJsonObject windows{{"id", "demo-windows"}, {"name", "Windows-PC"}, {"kind", "engine"}, {"address", "https://192.0.2.30:43871"}, {"fingerprint", "bb"}, {"paired", true}, {"online", false}, {"canSend", true}, {"canReceive", true}, {"canClipboard", true}, {"autoAccept", true}, {"platform", "windows"}, {"form", "desktop"}};
  const QJsonObject nearby{{"id", "demo-air"}, {"name", "MacBook Air"}, {"kind", "engine"}, {"address", "https://192.0.2.41:43871"}, {"fingerprint", "cc"}, {"paired", false}, {"online", true}, {"canSend", false}, {"canReceive", false}, {"autoAccept", false}, {"platform", "macos"}, {"form", "laptop"}};
  if (scenario == "welcome" || scenario == "empty")
    devices = {nearby};
  else {
    devices = {linux_, windows};
    if (scenario == "home")
      devices.append(nearby);
    transfers.append(QJsonObject{{"id", "t1"}, {"deviceId", "demo-linux"}, {"deviceName", "MacBook Pro"}, {"direction", "send"}, {"state", "running"}, {"files", QJsonArray{QJsonObject{{"id", "f1"}, {"path", "Urlaub 2026.mov"}, {"size", 2147483648.0}}}}, {"totalBytes", 2147483648.0}, {"completedBytes", 1331439862.0}, {"bytesPerSecond", 48234496.0}, {"createdAt", ago(40)}, {"updatedAt", ago(0)}, {"fileCount", 1}, {"summary", true}});
    transfers.append(QJsonObject{{"id", "t2"}, {"deviceId", "demo-linux"}, {"deviceName", "MacBook Pro"}, {"direction", "receive"}, {"state", "completed"}, {"files", QJsonArray{QJsonObject{{"id", "f2"}, {"path", "Notizen.pdf"}, {"size", 482133}, {"completed", true}}}}, {"totalBytes", 482133}, {"completedBytes", 482133}, {"createdAt", ago(300)}, {"updatedAt", ago(240)}, {"fileCount", 1}, {"summary", true}, {"hasContent", true}});
    transfers.append(QJsonObject{{"id", "t3"}, {"deviceId", "demo-windows"}, {"deviceName", "Windows-PC"}, {"direction", "send"}, {"state", "completed"}, {"files", QJsonArray{QJsonObject{{"id", "f3"}, {"path", "Fotos"}, {"directory", true}, {"completed", true}}, QJsonObject{{"id", "f4"}, {"path", "Fotos/IMG_0001.jpg"}, {"completed", true}}}}, {"totalBytes", 734003200}, {"completedBytes", 734003200}, {"createdAt", ago(7300)}, {"updatedAt", ago(7200)}, {"fileCount", 214}, {"summary", true}});
    if (scenario == "home")
      clipboard.append(QJsonObject{{"id", "demo-clip"}, {"deviceId", "demo-linux"}, {"deviceName", "MacBook Pro"}, {"direction", "receive"}, {"text", ""}, {"createdAt", ago(20)}});
    else
      pairings.append(QJsonObject{{"id", "demo-pair"}, {"peerId", "demo-air"}, {"peerName", "MacBook Air"}, {"state", "comparing"}, {"comparisonCode", "48210937"}, {"expiresAt", now.addSecs(240).toString(Qt::ISODate)}, {"localConfirmed", false}, {"remoteConfirmed", false}, {"automaticReceive", true}, {"initiator", false}, {"platform", "macos"}, {"form", "laptop"}});
  }
  const QString name = en ? "Linux PC" : "Linux-PC";
  return {{"summary", true}, {"version", QString::fromLatin1(Version)}, {"id", "demo-self"}, {"name", name}, {"urls", QJsonArray{"https://192.0.2.22:43871", "https://localhost:43871"}}, {"fingerprint", "dd"}, {"devices", devices}, {"transfers", transfers}, {"pairingSessions", pairings}, {"clipboard", clipboard},
          {"settings", QJsonObject{{"setupCompleted", scenario != "welcome"}, {"name", name}, {"receiveDirectory", QDir::homePath() + "/Downloads"}, {"appearance", "system"}, {"language", en ? "en" : "de"}, {"notifications", true}, {"automaticUpdateChecks", true}, {"sendLimitBytesPerSecond", 0}, {"receiveLimitBytesPerSecond", 0}}}};
}
bool save(QWidget *widget, const QString &path) {
  widget->ensurePolished();
  widget->adjustSize();
  QApplication::processEvents();
  QApplication::processEvents();
  return widget->grab().save(path);
}
} // namespace

bool MainWindow::snapshot(const QString &directory) {
  if (!demoMode || !QDir().mkpath(directory))
    return false;
  const bool en = qEnvironmentVariable("AIRLINK_DEMO_LANGUAGE") == "en";
  bool ok = true;
  resize(720, 700);
  show();
  for (const auto &scenario : {QStringLiteral("welcome"), QStringLiteral("empty"), QStringLiteral("home")}) {
    client.showDemo(demoStatus(scenario, en));
    if (scenario == "home")
      clipboardTexts.insert("demo-clip", "https://example.com/notizen");
    bannerKeys.clear();
    render();
    QApplication::processEvents();
    QApplication::processEvents();
    ok = grab().save(QDir(directory).filePath(scenario + ".png")) && ok;
  }
  client.showDemo(demoStatus("pairing", en));
  render();
  PairingDialog pairingView("demo-pair", this);
  pairingView.setAttribute(Qt::WA_DeleteOnClose, false);
  pairingView.refresh(pairing("demo-pair"), english());
  pairingView.show();
  ok = save(&pairingView, QDir(directory).filePath("pairing.png")) && ok;
  pairingView.hide();
  AddDeviceDialog add(true, english(), this);
  add.setAttribute(Qt::WA_DeleteOnClose, false);
  add.setNearby(QJsonArray{demoStatus("empty", en)["devices"].toArray().first()});
  add.setOwnAddress("192.0.2.22");
  add.show();
  ok = save(&add, QDir(directory).filePath("add.png")) && ok;
  add.hide();
  openSettings(0);
  if (settingsDialog) {
    settingsDialog->setAttribute(Qt::WA_DeleteOnClose, false);
    ok = save(settingsDialog, QDir(directory).filePath("settings.png")) && ok;
    settingsDialog->hide();
  }
  return ok;
}
} // namespace AirLink
