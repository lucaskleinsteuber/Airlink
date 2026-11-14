#include "window.h"
#include "dialogs.h"
#include "version.h"
#include "widgets.h"
#include <QAction>
#include <QApplication>
#include <QCheckBox>
#include <QClipboard>
#include <QCloseEvent>
#include <QDBusConnection>
#include <QDBusInterface>
#include <QDBusPendingCallWatcher>
#include <QDBusPendingReply>
#include <QDesktopServices>
#include <QDir>
#include <QFile>
#include <QFileDialog>
#include <QFileInfo>
#include <QGridLayout>
#include <QHBoxLayout>
#include <QJsonDocument>
#include <QLabel>
#include <QLineEdit>
#include <QLocale>
#include <QMenu>
#include <QMenuBar>
#include <QMessageBox>
#include <QPushButton>
#include <QRegularExpression>
#include <QSaveFile>
#include <QScrollArea>
#include <QStackedWidget>
#include <QStandardPaths>
#include <QSysInfo>
#include <QToolBar>
#include <QToolButton>
#include <QUrl>
#include <QVBoxLayout>

namespace AirLink {
namespace {
const char *autostartName = "io.github.lucaskleinsteuber.AirLink.desktop";
} // namespace

MainWindow::MainWindow(bool demo, QWidget *parent)
    : QMainWindow(parent), client(this), tray(this), network(this),
      demoMode(demo) {
  setWindowTitle("AirLink");
  setWindowIcon(QIcon(":/airlink/icon.svg"));
  resize(720, 680);
  setMinimumSize(560, 500);
  builtEnglish = english();
  build();
  connect(&client, &DaemonClient::statusChanged, this, &MainWindow::render);
  connect(&client, &DaemonClient::failed, this, [this](const QString &e) {
    startupError = e;
    preparing.clear();
    render();
  });
  tray.setIcon(windowIcon());
  tray.setToolTip("AirLink");
  rebuildTrayMenu();
  connect(&tray, &QSystemTrayIcon::activated, this,
          [this](QSystemTrayIcon::ActivationReason r) {
            if (r == QSystemTrayIcon::Trigger)
              activate();
          });
  if (!demo)
    tray.show();
  listenTimer.setInterval(60000);
  connect(&listenTimer, &QTimer::timeout, this, [this] {
    if (client.ready() && pairingOpen)
      client.request("pairing.listen", {}, [](QJsonValue, QString) {});
  });
  if (!demo)
    QDBusConnection::sessionBus().connect(
        "org.freedesktop.Notifications", "/org/freedesktop/Notifications",
        "org.freedesktop.Notifications", "ActionInvoked", this,
        SLOT(notificationAction(uint, QString)));
}

bool MainWindow::english() const {
  const auto lang = settings()["language"].toString("system");
  if (lang == "en")
    return true;
  if (lang == "de")
    return false;
  return QLocale::system().language() != QLocale::German;
}
QString MainWindow::text(const char *de, const char *en) const {
  return QString::fromUtf8(english() ? en : de);
}
QJsonObject MainWindow::settings() const {
  return client.status()["settings"].toObject();
}
QJsonArray MainWindow::pairedDevices() const {
  QJsonArray out;
  for (const auto &v : sortedDevices(client.status()["devices"].toArray(), true))
    if (v.toObject()["kind"] == "engine")
      out.append(v);
  return out;
}
QJsonArray MainWindow::nearbyDevices() const {
  QJsonArray out;
  for (const auto &v : sortedDevices(client.status()["devices"].toArray(), false)) {
    const auto d = v.toObject();
    if (!d["paired"].toBool() && d["kind"] == "engine" &&
        !d["address"].toString().isEmpty())
      out.append(d);
  }
  return out;
}
QJsonObject MainWindow::device(const QString &id) const {
  for (const auto &v : client.status()["devices"].toArray())
    if (v.toObject()["id"].toString() == id)
      return v.toObject();
  return {};
}
QJsonObject MainWindow::pairing(const QString &id) const {
  for (const auto &v : client.status()["pairingSessions"].toArray())
    if (v.toObject()["id"].toString() == id)
      return v.toObject();
  return {};
}
QStringList MainWindow::localAddresses() const {
  QStringList out;
  for (const auto &v : client.status()["urls"].toArray()) {
    const QUrl url(v.toString());
    if (url.host() != "localhost" && !url.host().contains(':'))
      out.append(url.host());
  }
  return out;
}

void MainWindow::build() {
  if (toolbar) {
    removeToolBar(toolbar);
    toolbar->deleteLater();
  }
  toolbar = addToolBar("AirLink");
  toolbar->setObjectName("main");
  toolbar->setMovable(false);
  toolbar->setFloatable(false);
  toolbar->setToolButtonStyle(Qt::ToolButtonTextBesideIcon);
  toolbar->setContextMenuPolicy(Qt::PreventContextMenu);
  auto *send = toolbar->addAction(themedIcon("document-send"),
                                  text("Dateien senden …", "Send Files …"));
  send->setShortcut(QKeySequence::Open);
  send->setObjectName("send");
  connect(send, &QAction::triggered, this, [this] { chooseFilesForAnyDevice(); });
  auto *spacer = new QWidget(toolbar);
  spacer->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Preferred);
  toolbar->addWidget(spacer);
  auto *history = toolbar->addAction(themedIcon("view-history"), text("Verlauf", "History"));
  history->setObjectName("history");
  connect(history, &QAction::triggered, this, [this] { openHistory(); });
  auto *help = toolbar->addAction(themedIcon("help-contents"), text("Hilfe", "Help"));
  help->setShortcut(QKeySequence::HelpContents);
  connect(help, &QAction::triggered, this, [this] { showHelp(); });
  auto *configure = toolbar->addAction(themedIcon("configure"), text("Einstellungen", "Settings"));
  configure->setShortcut(QKeySequence::Preferences);
  connect(configure, &QAction::triggered, this, [this] { openSettings(0); });
  auto *quitAction = new QAction(toolbar);
  quitAction->setShortcut(QKeySequence::Quit);
  connect(quitAction, &QAction::triggered, this, &MainWindow::quit);
  toolbar->addAction(quitAction);
  quitAction->setVisible(false);
  menuBar()->hide();
  tileKeys.clear();
  bannerKeys.clear();
  tileMap.clear();
  rowMap.clear();
  rowOrder.clear();
  welcomeFilled = false;
  auto *previous = pages;
  pages = new QStackedWidget(this);
  pages->addWidget(buildStartup());
  pages->addWidget(buildWelcome());
  pages->addWidget(buildHome());
  setCentralWidget(pages);
  if (previous)
    previous->deleteLater();
}

QWidget *MainWindow::buildStartup() {
  auto *page = new QWidget;
  auto *v = new QVBoxLayout(page);
  v->setContentsMargins(40, 40, 40, 40);
  v->addStretch();
  startupTitle = styledLabel(text("AirLink startet …", "Starting AirLink …"), page, 1.5, true);
  startupTitle->setAlignment(Qt::AlignCenter);
  v->addWidget(startupTitle);
  startupText = styledLabel("", page, 1.0, false, true);
  startupText->setAlignment(Qt::AlignCenter);
  v->addWidget(startupText);
  auto *row = new QHBoxLayout;
  row->addStretch();
  startupFolder = button(text("Anderen Empfangsordner wählen …", "Choose Another Receive Folder …"), page, [this] { chooseReceiveFolder(); });
  startupRetry = button(text("Noch einmal versuchen", "Try Again"), page, [this] {
    startupError.clear();
    client.shutdown([this](bool) { start(); });
    render();
  });
  startupRetry->setDefault(true);
  row->addWidget(startupFolder);
  row->addWidget(startupRetry);
  row->addStretch();
  v->addSpacing(12);
  v->addLayout(row);
  v->addStretch();
  return page;
}

QWidget *MainWindow::buildWelcome() {
  auto *scroll = new QScrollArea;
  scroll->setWidgetResizable(true);
  scroll->setFrameShape(QFrame::NoFrame);
  auto *outer = new QWidget;
  auto *center = new QHBoxLayout(outer);
  auto *column = new QWidget(outer);
  column->setMaximumWidth(560);
  center->addStretch();
  center->addWidget(column, 1);
  center->addStretch();
  auto *v = new QVBoxLayout(column);
  v->setContentsMargins(24, 30, 24, 30);
  v->setSpacing(14);
  auto *logo = new QLabel(column);
  logo->setPixmap(QIcon(":/airlink/icon.svg").pixmap(96, 96));
  logo->setAlignment(Qt::AlignCenter);
  v->addWidget(logo);
  auto *title = styledLabel(text("Willkommen bei AirLink", "Welcome to AirLink"), column, 1.9, true);
  title->setAlignment(Qt::AlignCenter);
  v->addWidget(title);
  auto *intro = styledLabel(text("Schick Dateien direkt zwischen deinen Computern – Linux, Mac und Windows. Schnell, verschlüsselt und nur in deinem Netzwerk.",
                                 "Send files directly between your computers – Linux, Mac and Windows. Fast, encrypted and only on your network."),
                            column, 1.0, false, true);
  intro->setAlignment(Qt::AlignCenter);
  v->addWidget(intro);
  v->addSpacing(8);
  auto *card = new Card(column);
  auto *grid = new QGridLayout(card);
  grid->setContentsMargins(18, 14, 18, 14);
  grid->setHorizontalSpacing(14);
  grid->setVerticalSpacing(12);
  grid->addWidget(styledLabel(text("Name dieses Computers", "Name of this computer"), card, 1.0), 0, 0);
  welcomeName = new QLineEdit(card);
  welcomeName->setMaxLength(120);
  grid->addWidget(welcomeName, 0, 1);
  grid->addWidget(styledLabel(text("Empfangene Dateien", "Received files"), card, 1.0), 1, 0);
  auto *folderRow = new QHBoxLayout;
  welcomeFolder = styledLabel("", card, 0.95, false, true);
  welcomeFolder->setWordWrap(false);
  folderRow->addWidget(welcomeFolder, 1);
  folderRow->addWidget(button(text("Ändern …", "Change …"), card, [this] {
    const auto chosen = QFileDialog::getExistingDirectory(this, text("Wohin sollen empfangene Dateien?", "Where should received files go?"), welcomePath);
    if (!chosen.isEmpty()) {
      welcomePath = chosen;
      welcomeFolder->setText(QDir::toNativeSeparators(chosen).replace(QDir::homePath(), "~"));
    }
  }));
  grid->addLayout(folderRow, 1, 1);
  welcomeAutostart = new QCheckBox(text("Beim Anmelden starten", "Open at login"), card);
  welcomeAutostart->setChecked(true);
  grid->addWidget(welcomeAutostart, 2, 0, 1, 2);
  auto *autostartHint = styledLabel(text("Damit dich deine anderen Computer jederzeit erreichen.", "So your other computers can always reach you."), card, 0.9, false, true);
  autostartHint->setContentsMargins(26, 0, 0, 0);
  grid->addWidget(autostartHint, 3, 0, 1, 2);
  welcomeNotify = new QCheckBox(text("Mitteilungen", "Notifications"), card);
  welcomeNotify->setChecked(true);
  grid->addWidget(welcomeNotify, 4, 0, 1, 2);
  auto *notifyHint = styledLabel(text("Wenn Dateien ankommen oder jemand sich verbinden möchte.", "When files arrive or someone wants to connect."), card, 0.9, false, true);
  notifyHint->setContentsMargins(26, 0, 0, 0);
  grid->addWidget(notifyHint, 5, 0, 1, 2);
  grid->setColumnStretch(1, 1);
  v->addWidget(card);
  v->addSpacing(8);
  auto *go = button(text("Los geht’s", "Get Started"), column, [this] { finishWelcome(); });
  go->setDefault(true);
  go->setMinimumWidth(200);
  go->setMinimumHeight(go->sizeHint().height() + 8);
  v->addWidget(go, 0, Qt::AlignHCenter);
  connect(welcomeName, &QLineEdit::returnPressed, this, [this] { finishWelcome(); });
  v->addStretch();
  scroll->setWidget(outer);
  return scroll;
}

QWidget *MainWindow::buildHome() {
  auto *scroll = new QScrollArea;
  scroll->setWidgetResizable(true);
  scroll->setFrameShape(QFrame::NoFrame);
  auto *content = new QWidget;
  auto *v = new QVBoxLayout(content);
  v->setContentsMargins(28, 18, 28, 24);
  v->setSpacing(10);
  bannerBox = new QVBoxLayout;
  bannerBox->setSpacing(10);
  v->addLayout(bannerBox);
  homeTitle = styledLabel("", content, 1.45, true);
  v->addWidget(homeTitle);
  homeSubtitle = styledLabel("", content, 1.0, false, true);
  v->addWidget(homeSubtitle);
  v->addSpacing(10);
  tileHost = new QWidget(content);
  tiles = new FlowLayout(tileHost, 16);
  v->addWidget(tileHost);
  ipLink = new QLabel(content);
  ipLink->setTextFormat(Qt::RichText);
  ipLink->setText("<a href=\"ip\">" + text("Nicht gefunden? Per IP-Adresse verbinden …", "Not found? Connect by IP address …").toHtmlEscaped() + "</a>");
  connect(ipLink, &QLabel::linkActivated, this, [this] { showAddDevice(true); });
  v->addWidget(ipLink);
  recentArea = new QWidget(content);
  auto *rv = new QVBoxLayout(recentArea);
  rv->setContentsMargins(0, 14, 0, 0);
  rv->setSpacing(8);
  auto *header = new QHBoxLayout;
  header->addWidget(styledLabel(text("Zuletzt", "Recent"), recentArea, 1.1, true));
  header->addStretch();
  auto *all = new QLabel("<a href=\"all\">" + text("Alle anzeigen", "Show all").toHtmlEscaped() + "</a>", recentArea);
  all->setTextFormat(Qt::RichText);
  connect(all, &QLabel::linkActivated, this, [this] { openHistory(); });
  header->addWidget(all);
  rv->addLayout(header);
  auto *card = new Card(recentArea);
  recentList = new QVBoxLayout(card);
  recentList->setContentsMargins(0, 3, 0, 3);
  recentList->setSpacing(0);
  rv->addWidget(card);
  v->addWidget(recentArea);
  v->addStretch(1);
  scroll->setWidget(content);
  return scroll;
}

void MainWindow::rebuildTrayMenu() {
  auto *previous = tray.contextMenu();
  auto *menu = new QMenu(this);
  menu->addAction(text("AirLink öffnen", "Open AirLink"), this, [this] { activate(); });
  menu->addAction(text("Dateien senden …", "Send Files …"), this, [this] {
    activate();
    chooseFilesForAnyDevice();
  });
  menu->addSeparator();
  menu->addAction(text("Beenden", "Quit"), this, &MainWindow::quit);
  tray.setContextMenu(menu);
  if (previous)
    previous->deleteLater();
}

void MainWindow::activate() {
  showNormal();
  raise();
  activateWindow();
}

void MainWindow::start() {
  if (demoMode)
    return;
  helperPath = QDir(QCoreApplication::applicationDirPath())
                   .absoluteFilePath("../libexec/airlink/airlink-service");
  if (const auto custom = qEnvironmentVariable("AIRLINK_HELPER"); !custom.isEmpty())
    helperPath = custom;
  if (!QFileInfo(helperPath).isExecutable()) {
    startupError = text("Ein Teil von AirLink fehlt. Bitte AirLink neu installieren.",
                        "Part of AirLink is missing. Please reinstall AirLink.");
    render();
    return;
  }
  const QString data = qEnvironmentVariable("AIRLINK_DATA_DIR", dataDirectory());
  const bool fresh = !QFileInfo(QDir(data).filePath("settings.json")).exists();
  client.start(helperPath, data, false, fresh ? downloadsDirectory() : QString{});
}

void MainWindow::render() {
  if (english() != builtEnglish) {
    builtEnglish = english();
    build();
    rebuildTrayMenu();
  }
  const auto status = client.status();
  if (!client.ready() || status.isEmpty()) {
    pages->setCurrentIndex(0);
    const bool failed = !startupError.isEmpty();
    startupTitle->setText(failed ? text("AirLink konnte nicht starten", "AirLink could not start")
                                 : text("AirLink startet …", "Starting AirLink …"));
    QString detail;
    if (failed) {
      if (startupError.contains("already running"))
        detail = text("AirLink läuft schon. Beende die andere AirLink-App und versuch es noch einmal.", "AirLink is already running. Quit the other AirLink app and try again.");
      else if (startupError.contains("receive", Qt::CaseInsensitive) || startupError.contains("directory", Qt::CaseInsensitive))
        detail = text("Der Empfangsordner ist nicht erreichbar. Wähle einen anderen Ordner, z. B. „Downloads“.", "The receive folder is not available. Choose another folder, such as Downloads.");
      else
        detail = text("Ein Neustart von AirLink hilft meistens.", "Restarting AirLink usually helps.") + "\n\n" + startupError;
    }
    startupText->setText(detail);
    startupRetry->setVisible(failed);
    startupFolder->setVisible(failed);
    return;
  }
  const auto current = settings();
  for (auto *a : toolbar->actions())
    if (a->objectName() == "send" || a->objectName() == "history")
      a->setEnabled(current["setupCompleted"].toBool());
  if (!current["setupCompleted"].toBool()) {
    pages->setCurrentIndex(1);
    if (!welcomeFilled) {
      welcomeFilled = true;
      QString name = current["name"].toString();
      if (name.isEmpty() || name == "AirLink" || name == "Linux")
        name = QSysInfo::machineHostName();
      welcomeName->setText(name);
      welcomePath = current["receiveDirectory"].toString(downloadsDirectory());
      welcomeFolder->setText(QDir::toNativeSeparators(welcomePath).replace(QDir::homePath(), "~"));
      welcomeAutostart->setChecked(demoMode || !autostartEnabled() ? true : autostartEnabled());
      welcomeNotify->setChecked(current["notifications"].toBool(true));
    }
  } else {
    pages->setCurrentIndex(2);
    const bool none = pairedDevices().isEmpty();
    homeTitle->setText(none ? text("Verbinde deinen anderen Computer", "Connect your other computer")
                            : text("Zieh Dateien auf einen Computer", "Drag files onto a computer"));
    homeSubtitle->setText(none ? text("Öffne AirLink auf dem anderen Computer. Er erscheint dann hier – klick ihn an und vergleicht einen Code. Das musst du nur einmal machen.",
                                      "Open AirLink on the other computer. It then appears here – click it and compare a code. You only do this once.")
                               : text("Oder klick ihn an, um Dateien auszuwählen. Alles bleibt in deinem Netzwerk.",
                                      "Or click it to choose files. Everything stays on your network."));
    ipLink->setVisible(none);
    renderBanners();
    renderTiles();
    renderRecent();
  }
  observe(lastStatus, status);
  lastStatus = status;
  if (pairingDialog)
    pairingDialog->refresh(pairing(pairingDialog->id()), english());
  if (addDialog) {
    addDialog->setNearby(nearbyDevices());
    addDialog->setOwnAddress(localAddresses().value(0));
  }
  int active = 0;
  for (const auto &v : status["transfers"].toArray())
    if (!terminal(v.toObject()["state"].toString()))
      ++active;
  tray.setToolTip(active > 0 ? (english() ? QString("AirLink · %1 active").arg(active) : QString("AirLink · %1 aktiv").arg(active)) : QStringLiteral("AirLink"));
  if (!initialized) {
    initialized = true;
    updatePairingWindow();
    if (!pendingPaths.isEmpty()) {
      const auto paths = pendingPaths;
      pendingPaths.clear();
      acceptPaths(paths);
    }
  }
  if (!demoMode)
    maybeCheckUpdates();
}

void MainWindow::renderBanners() {
  const auto status = client.status();
  QStringList keys;
  QList<QJsonObject> offers;
  for (const auto &v : status["transfers"].toArray()) {
    const auto t = v.toObject();
    if (t["direction"] == "receive" && t["state"] == "offered" && offers.size() < 2) {
      offers.append(t);
      keys.append("o:" + t["id"].toString());
    }
  }
  QJsonObject clip;
  int clips = 0;
  for (const auto &v : status["clipboard"].toArray()) {
    const auto m = v.toObject();
    if (m["direction"] != "receive" || !clipboardTexts.contains(m["id"].toString()))
      continue;
    ++clips;
    if (clip.isEmpty() || m["createdAt"].toString() > clip["createdAt"].toString())
      clip = m;
  }
  if (!clip.isEmpty())
    keys.append("c:" + clip["id"].toString() + ':' + QString::number(clips));
  if (keys == bannerKeys)
    return;
  bannerKeys = keys;
  while (auto *item = bannerBox->takeAt(0)) {
    if (auto *w = item->widget()) {
      w->hide();
      w->deleteLater();
    }
    delete item;
  }
  for (const auto &t : offers) {
    const auto id = t["id"].toString();
    auto *banner = new Banner("folder-download",
                              english() ? t["deviceName"].toString() + " wants to send you something"
                                        : t["deviceName"].toString() + " möchte dir etwas senden",
                              describeTransfer(t, english()) + " · " + bytes(static_cast<qint64>(t["totalBytes"].toDouble())));
    banner->actions()->addWidget(button(text("Ablehnen", "Decline"), banner, [this, id] { transferAction("cancel", id); }));
    auto *accept = button(text("Annehmen", "Accept"), banner, [this, id] { transferAction("accept", id); });
    accept->setDefault(true);
    banner->actions()->addWidget(accept);
    bannerBox->addWidget(banner);
  }
  if (!clip.isEmpty()) {
    const auto id = clip["id"].toString();
    QString heading = (english() ? "Text from " : "Text von ") + clip["deviceName"].toString();
    if (clips > 1)
      heading += english() ? QString(" (+%1 more)").arg(clips - 1) : QString(" (+%1 weitere)").arg(clips - 1);
    auto *banner = new Banner("edit-paste", heading, clipboardTexts.value(id).left(400));
    auto dismiss = [this, id] {
      clipboardTexts.remove(id);
      client.request("clipboard.dismiss", {{"id", id}}, [this](QJsonValue, QString) { client.refresh(); });
      bannerKeys.clear();
      renderBanners();
    };
    auto *close = new QToolButton(banner);
    close->setAutoRaise(true);
    close->setIcon(themedIcon("window-close"));
    close->setToolTip(text("Verwerfen", "Dismiss"));
    connect(close, &QToolButton::clicked, this, dismiss);
    banner->actions()->addWidget(close);
    auto *copy = button(text("Kopieren", "Copy"), banner, [this, id, dismiss] {
      QGuiApplication::clipboard()->setText(clipboardTexts.value(id));
      dismiss();
    });
    copy->setDefault(true);
    banner->actions()->addWidget(copy);
    bannerBox->addWidget(banner);
  }
}

void MainWindow::renderTiles() {
  const auto paired = pairedDevices(), nearby = nearbyDevices();
  QStringList keys;
  for (const auto &v : paired)
    keys.append("p:" + v.toObject()["id"].toString());
  for (const auto &v : nearby)
    keys.append("n:" + v.toObject()["id"].toString());
  if (paired.isEmpty() && nearby.isEmpty())
    keys.append("search");
  keys.append("add");
  if (keys != tileKeys) {
    tiles->clear();
    tileMap.clear();
    tileKeys = keys;
    for (const auto &key : keys) {
      const auto id = key.mid(2);
      const auto kind = key.startsWith("p:")   ? DeviceTile::Kind::Paired
                        : key.startsWith("n:") ? DeviceTile::Kind::Nearby
                        : key == "search"      ? DeviceTile::Kind::Searching
                                               : DeviceTile::Kind::Add;
      auto *tile = new DeviceTile(kind, tileHost);
      if (kind == DeviceTile::Kind::Paired) {
        connect(tile, &DeviceTile::activated, this, [this, id] { chooseFiles(device(id)); });
        connect(tile, &DeviceTile::filesDropped, this, [this, id](const QStringList &paths) { send(paths, device(id)); });
        connect(tile, &DeviceTile::menuRequested, this, [this, id](const QPoint &at) { tileMenu(device(id), at); });
      } else if (kind == DeviceTile::Kind::Nearby) {
        connect(tile, &DeviceTile::activated, this, [this, id] { connectDevice(device(id)); });
        connect(tile, &DeviceTile::menuRequested, this, [this, id](const QPoint &at) {
          QMenu menu(this);
          menu.addAction(text("Verbinden", "Connect"), this, [this, id] { connectDevice(device(id)); });
          menu.exec(at);
        });
      } else {
        connect(tile, &DeviceTile::activated, this, [this] { showAddDevice(false); });
      }
      tiles->addWidget(tile);
      tileMap.insert(key, tile);
    }
  }
  const auto status = client.status();
  const auto transfers = status["transfers"].toArray(), pairings = status["pairingSessions"].toArray();
  QList<QJsonObject> shown;
  for (const auto &v : paired)
    shown.append(v.toObject());
  for (const auto &v : nearby)
    shown.append(v.toObject());
  for (const auto &d : shown) {
    const auto key = (d["paired"].toBool() ? "p:" : "n:") + d["id"].toString();
    if (auto *tile = tileMap.value(key)) {
      const auto act = deviceActivity(d, transfers, pairings, preparing.contains(d["id"].toString()), english());
      tile->setContent(deviceIconName(d["platform"].toString(), d["form"].toString()), d["name"].toString(),
                       act.label, act.progress, act.busy, !d["paired"].toBool() || d["online"].toBool());
      tile->setToolTip(d["paired"].toBool()
                           ? (english() ? "Click to send files to " + d["name"].toString() + ", or drag files here."
                                        : "Klicken, um Dateien an " + d["name"].toString() + " zu senden – oder Dateien hierher ziehen.")
                           : (english() ? "Click to connect to " + d["name"].toString() + ". You then compare a code."
                                        : "Klicken, um dich mit " + d["name"].toString() + " zu verbinden. Ihr vergleicht dann einen Code."));
    }
  }
  if (auto *tile = tileMap.value("search"))
    tile->setContent("network-wireless", text("Suche …", "Searching …"), text("Im Netzwerk", "On your network"), -1, false, true);
  if (auto *tile = tileMap.value("add")) {
    tile->setContent("list-add", text("Hinzufügen", "Add"), text("Anderer Computer", "Another computer"), -1, false, true);
    tile->setToolTip(text("Einen anderen Computer hinzufügen", "Add another computer"));
  }
  tileHost->updateGeometry();
}

void MainWindow::renderRecent() {
  const auto items = recentTransfers(client.status()["transfers"].toArray(), 5);
  QStringList order;
  for (const auto &v : items)
    order.append(v.toObject()["id"].toString());
  if (order != rowOrder) {
    rowOrder = order;
    rowMap.clear();
    while (auto *item = recentList->takeAt(0)) {
      if (auto *w = item->widget()) {
        w->hide();
        w->deleteLater();
      }
      delete item;
    }
    for (int i = 0; i < order.size(); ++i) {
      if (i > 0) {
        auto *line = new QFrame(recentList->parentWidget());
        line->setFrameShape(QFrame::HLine);
        line->setFrameShadow(QFrame::Plain);
        auto pal = line->palette();
        pal.setColor(QPalette::WindowText, mix(pal.color(QPalette::Base), pal.color(QPalette::WindowText), 0.10));
        line->setPalette(pal);
        line->setContentsMargins(64, 0, 0, 0);
        recentList->addWidget(line);
      }
      auto *row = new TransferRow(recentList->parentWidget());
      connect(row, &TransferRow::action, this, &MainWindow::transferAction);
      recentList->addWidget(row);
      rowMap.insert(order[i], row);
    }
  }
  for (const auto &v : items) {
    const auto t = v.toObject();
    if (auto *row = rowMap.value(t["id"].toString()))
      row->setTransfer(t, english());
  }
  recentArea->setVisible(!items.isEmpty());
}

void MainWindow::observe(const QJsonObject &previous, const QJsonObject &next) {
  const bool en = english();
  for (const auto &v : next["pairingSessions"].toArray()) {
    const auto p = v.toObject();
    const auto id = p["id"].toString();
    if (p["initiator"].toBool() || p["state"] != "comparing" || p["localConfirmed"].toBool() || shownPairings.contains(id))
      continue;
    shownPairings.insert(id);
    if (!pairingDialog || !pairing(pairingDialog->id())["state"].toString().contains(QRegularExpression("starting|comparing|finishing")))
      showPairing(id);
    activate();
    notify("pair-" + id, text("Verbindungsanfrage", "Connection request"),
           (en ? p["peerName"].toString() + " wants to connect to this computer."
               : p["peerName"].toString() + " möchte sich mit diesem Computer verbinden."));
  }
  bool unknownClip = false;
  for (const auto &v : next["clipboard"].toArray()) {
    const auto m = v.toObject();
    if (m["direction"] == "receive" && !clipboardTexts.contains(m["id"].toString()))
      unknownClip = true;
  }
  if (unknownClip && !demoMode)
    client.request("clipboard.list", {}, [this](QJsonValue value, QString error) {
      if (!error.isEmpty())
        return;
      for (const auto &v : value.toArray()) {
        const auto m = v.toObject();
        const auto id = m["id"].toString();
        if (m["direction"] != "receive" || clipboardTexts.contains(id))
          continue;
        clipboardTexts.insert(id, m["text"].toString());
        notify("clip-" + id, (english() ? "Text from " : "Text von ") + m["deviceName"].toString(), m["text"].toString().left(120));
      }
      bannerKeys.clear();
      renderBanners();
    });
  if (previous.isEmpty() || !settings()["notifications"].toBool(true))
    return;
  QHash<QString, QString> before;
  for (const auto &v : previous["transfers"].toArray())
    before.insert(v.toObject()["id"].toString(), v.toObject()["state"].toString());
  for (const auto &v : next["transfers"].toArray()) {
    const auto t = v.toObject();
    const auto id = t["id"].toString(), state = t["state"].toString();
    if (!before.contains(id) && state != "offered")
      continue;
    if (before.value(id) == state)
      continue;
    const auto name = describeTransfer(t, en), device = t["deviceName"].toString();
    if (state == "completed" && t["direction"] == "receive")
      notify("done-" + id, (en ? "Received from " : "Von ") + device + (en ? "" : " empfangen"), name, id);
    else if (state == "completed" && !isActiveWindow())
      notify("done-" + id, (en ? "Sent to " : "An ") + device + (en ? "" : " gesendet"), name);
    else if (state == "offered" && t["direction"] == "receive")
      notify("offer-" + id, device + (en ? " wants to send files" : " möchte Dateien senden"), name);
    else if (state == "failed")
      notify("failed-" + id, text("Übertragung fehlgeschlagen", "Transfer failed"), name);
  }
}

void MainWindow::updatePairingWindow() {
  if (demoMode || !client.ready())
    return;
  const bool open = isVisible() && !isMinimized();
  if (open && !pairingOpen) {
    pairingOpen = true;
    client.request("pairing.listen", {}, [](QJsonValue, QString) {});
    listenTimer.start();
  } else if (!open && pairingOpen) {
    pairingOpen = false;
    listenTimer.stop();
    client.request("pairing.listen", {{"close", true}}, [](QJsonValue, QString) {});
  }
}
void MainWindow::showEvent(QShowEvent *event) {
  QMainWindow::showEvent(event);
  updatePairingWindow();
}
void MainWindow::hideEvent(QHideEvent *event) {
  QMainWindow::hideEvent(event);
  updatePairingWindow();
}
void MainWindow::closeEvent(QCloseEvent *event) {
  if (quitting || demoMode) {
    event->accept();
    return;
  }
  hide();
  event->ignore();
  if (!QSystemTrayIcon::isSystemTrayAvailable())
    showMinimized();
}

void MainWindow::issue(const QString &error) {
  if (demoMode || error.isEmpty())
    return;
  QMessageBox box(QMessageBox::Warning, "AirLink", text("Das hat nicht geklappt", "That did not work"), QMessageBox::Ok, this);
  box.setInformativeText(error);
  box.exec();
}
void MainWindow::action(const QString &method, const QJsonObject &params) {
  client.request(method, params, [this](QJsonValue, QString error) {
    if (!error.isEmpty())
      issue(error);
    client.refresh();
  });
}
void MainWindow::transferAction(const QString &kind, const QString &id) {
  if (kind == "reveal")
    reveal(id);
  else if (kind == "retry" || kind == "resume")
    action("transfer.resume", {{"id", id}});
  else if (kind == "pause")
    action("transfer.pause", {{"id", id}});
  else if (kind == "cancel")
    action("transfer.cancel", {{"id", id}});
  else if (kind == "accept")
    action("transfer.accept", {{"id", id}});
}

void MainWindow::send(const QStringList &paths, const QJsonObject &d) {
  if (!client.ready() || paths.isEmpty() || d.isEmpty())
    return;
  if (!d["canReceive"].toBool()) {
    issue(english() ? d["name"].toString() + " may not receive files from this computer. You can change this in Settings under Devices."
                    : d["name"].toString() + " darf von diesem Computer keine Dateien empfangen. Du kannst das in den Einstellungen unter „Geräte“ ändern.");
    return;
  }
  const auto batch = sendBatch(d, paths);
  if (batch.isEmpty()) {
    issue(text("Nur Dateien und Ordner auf diesem Computer können gesendet werden.", "Only files and folders on this computer can be sent."));
    return;
  }
  const auto id = d["id"].toString(), name = d["name"].toString();
  preparing.insert(id);
  renderTiles();
  client.request("preparation.start", batch, [this, id, name](QJsonValue value, QString error) {
    if (!error.isEmpty()) {
      preparing.remove(id);
      renderTiles();
      issue((english() ? "Sending to " + name + " did not work. " : "Senden an " + name + " hat nicht geklappt. ") + error);
      return;
    }
    waitForPreparation(value.toObject()["id"].toString(), id, name);
  });
}
void MainWindow::waitForPreparation(const QString &prepId, const QString &deviceId, const QString &name) {
  client.request("preparation.status", {{"id", prepId}}, [this, prepId, deviceId, name](QJsonValue value, QString error) {
    const auto state = value.toObject()["state"].toString();
    if (error.isEmpty() && state == "preparing") {
      QTimer::singleShot(150, this, [this, prepId, deviceId, name] { waitForPreparation(prepId, deviceId, name); });
      return;
    }
    preparing.remove(deviceId);
    client.refresh();
    renderTiles();
    if (!error.isEmpty() || state != "completed")
      issue((english() ? "Sending to " + name + " did not work. " : "Senden an " + name + " hat nicht geklappt. ") +
            (error.isEmpty() ? value.toObject()["error"].toString(text("Das Senden wurde abgebrochen.", "Sending was cancelled.")) : error));
  });
}
void MainWindow::chooseFiles(const QJsonObject &d, bool folders) {
  if (d.isEmpty())
    return;
  const auto prompt = english() ? "Send to " + d["name"].toString() : "An " + d["name"].toString() + " senden";
  QStringList paths;
  if (folders) {
    const auto folder = QFileDialog::getExistingDirectory(this, prompt, QDir::homePath());
    if (!folder.isEmpty())
      paths.append(folder);
  } else
    paths = QFileDialog::getOpenFileNames(this, prompt, QDir::homePath());
  send(paths, d);
}
void MainWindow::chooseFilesForAnyDevice() {
  QJsonArray receivers;
  for (const auto &v : pairedDevices())
    if (v.toObject()["canReceive"].toBool())
      receivers.append(v);
  if (receivers.isEmpty()) {
    issue(text("Verbinde zuerst deinen anderen Computer. Danach kannst du Dateien senden.", "Connect your other computer first. Then you can send files."));
    return;
  }
  if (receivers.size() == 1) {
    chooseFiles(receivers.first().toObject());
    return;
  }
  const auto paths = QFileDialog::getOpenFileNames(this, text("Dateien senden", "Send files"), QDir::homePath());
  if (!paths.isEmpty())
    sendChooser(paths);
}
void MainWindow::acceptPaths(const QStringList &paths) {
  if (paths.isEmpty())
    return;
  if (!client.ready() || !initialized) {
    pendingPaths += paths;
    return;
  }
  activate();
  QJsonArray receivers;
  for (const auto &v : pairedDevices())
    if (v.toObject()["canReceive"].toBool())
      receivers.append(v);
  if (receivers.isEmpty()) {
    issue(text("Verbinde zuerst deinen anderen Computer. Danach kannst du Dateien senden.", "Connect your other computer first. Then you can send files."));
    return;
  }
  if (receivers.size() == 1)
    send(paths, receivers.first().toObject());
  else
    sendChooser(paths);
}
void MainWindow::tileMenu(const QJsonObject &d, const QPoint &global) {
  if (d.isEmpty())
    return;
  const auto id = d["id"].toString();
  QMenu menu(this);
  menu.addAction(themedIcon("document-send"), text("Dateien senden …", "Send Files …"), this, [this, id] { chooseFiles(device(id)); });
  menu.addAction(themedIcon("folder"), text("Ordner senden …", "Send Folder …"), this, [this, id] { chooseFiles(device(id), true); });
  auto *clip = menu.addAction(themedIcon("edit-paste"), text("Zwischenablage senden", "Send Clipboard"), this, [this, id] { sendClipboard(device(id)); });
  clip->setEnabled(d["canClipboard"].toBool());
  menu.addSeparator();
  menu.addAction(themedIcon("configure"), (english() ? "Settings for " : "Einstellungen für ") + d["name"].toString() + " …", this, [this, id] { deviceSettings(id); });
  menu.addAction(themedIcon("edit-delete"), text("Entfernen …", "Remove …"), this, [this, id] { removeDevice(device(id)); });
  menu.exec(global);
}

void MainWindow::connectDevice(const QJsonObject &d) {
  if (d.isEmpty() || !client.ready())
    return;
  if (d["outdated"].toBool()) {
    issue(english() ? d["name"].toString() + " runs an older AirLink. Update AirLink there to 0.13 or later, then connect again."
                    : d["name"].toString() + " hat eine ältere AirLink-Version. Aktualisiere AirLink dort auf 0.13 oder neuer, dann klappt das Verbinden.");
    return;
  }
  QJsonObject params{{"url", d["address"]}, {"fingerprint", d["fingerprint"]}, {"name", d["name"]}};
  if (d.contains("platform"))
    params.insert("platform", d["platform"]);
  if (d.contains("form"))
    params.insert("form", d["form"]);
  client.request("pairing.start", params, [this](QJsonValue value, QString error) {
    if (!error.isEmpty()) {
      issue(text("Verbinden hat nicht geklappt: ", "Could not connect: ") + error);
      return;
    }
    if (addDialog)
      addDialog->close();
    showPairing(value.toObject()["id"].toString());
    client.refresh();
  });
}
void MainWindow::connectAddress(const QString &address, std::function<void(QString)> failed) {
  client.request("pairing.start", {{"address", address}}, [this, failed](QJsonValue value, QString error) {
    if (!error.isEmpty()) {
      failed(text("Diese Adresse sieht nicht richtig aus. Gib die IP-Adresse ein, die AirLink auf dem anderen Computer unter „Hilfe“ anzeigt, z. B. 192.168.178.22.",
                  "This address does not look right. Enter the IP address AirLink shows under Help on the other computer, for example 192.168.178.22."));
      return;
    }
    if (addDialog)
      addDialog->close();
    showPairing(value.toObject()["id"].toString());
    client.refresh();
  });
}
void MainWindow::showPairing(const QString &id) {
  if (id.isEmpty())
    return;
  if (pairingDialog && pairingDialog->id() == id) {
    pairingDialog->raise();
    pairingDialog->activateWindow();
    return;
  }
  if (pairingDialog)
    pairingDialog->close();
  auto *dialog = new PairingDialog(id, this);
  pairingDialog = dialog;
  connect(dialog, &PairingDialog::confirmRequested, this, [this](const QString &sid, bool automatic) {
    client.request("pairing.confirm", {{"id", sid}, {"automaticReceive", automatic}}, [this](QJsonValue, QString error) {
      if (!error.isEmpty())
        issue(text("Bestätigen hat nicht geklappt. Die Anfrage ist vielleicht abgelaufen – versuch es noch einmal.", "Confirming did not work. The request may have expired – please try again."));
      client.refresh();
    });
  });
  connect(dialog, &PairingDialog::cancelRequested, this, [this, dialog](const QString &sid) {
    client.request("pairing.cancel", {{"id", sid}}, [this](QJsonValue, QString) { client.refresh(); });
    dialog->close();
  });
  dialog->refresh(pairing(id), english());
  dialog->show();
  dialog->raise();
}
void MainWindow::showAddDevice(bool byAddress) {
  if (addDialog) {
    addDialog->raise();
    return;
  }
  auto *dialog = new AddDeviceDialog(byAddress, english(), this);
  addDialog = dialog;
  connect(dialog, &AddDeviceDialog::connectDevice, this, [this](const QJsonObject &d) { connectDevice(d); });
  connect(dialog, &AddDeviceDialog::connectAddress, this, [this, dialog](const QString &address) {
    QPointer<AddDeviceDialog> guard(dialog);
    connectAddress(address, [guard](QString message) {
      if (guard)
        guard->showIssue(message);
    });
  });
  dialog->setNearby(nearbyDevices());
  dialog->setOwnAddress(localAddresses().value(0));
  dialog->show();
}
void MainWindow::updateDevice(QJsonObject d) {
  action("device.update", d);
}
void MainWindow::removeDevice(const QJsonObject &d) {
  if (d.isEmpty())
    return;
  QMessageBox box(QMessageBox::Question, "AirLink",
                  (english() ? "Remove " : "") + d["name"].toString() + (english() ? "?" : " entfernen?"),
                  QMessageBox::NoButton, this);
  box.setInformativeText(text("Ihr könnt euch danach keine Dateien mehr schicken, bis ihr euch neu verbindet. Bereits gesendete Dateien bleiben erhalten.",
                              "You can no longer send each other files until you connect again. Files already sent stay where they are."));
  auto *remove = box.addButton(text("Entfernen", "Remove"), QMessageBox::DestructiveRole);
  box.addButton(QMessageBox::Cancel);
  box.exec();
  if (box.clickedButton() == remove)
    action("device.revoke", {{"id", d["id"]}});
}
void MainWindow::sendClipboard(const QJsonObject &d) {
  const auto value = QGuiApplication::clipboard()->text();
  if (value.isEmpty() || value.toUtf8().size() > 64 * 1024) {
    issue(text("In der Zwischenablage ist kein passender Text. Kopiere zuerst einen Text (Strg+C).", "There is no suitable text on the clipboard. Copy some text first (Ctrl+C)."));
    return;
  }
  const auto name = d["name"].toString();
  client.request("clipboard.send", {{"deviceId", d["id"]}, {"text", value}}, [this, name](QJsonValue, QString error) {
    if (!error.isEmpty())
      issue(text("Text konnte nicht gesendet werden: ", "Text could not be sent: ") + error);
    else
      tray.showMessage("AirLink", (english() ? "Text sent to " : "Text an ") + name + (english() ? "" : " gesendet"));
  });
}
void MainWindow::reveal(const QString &id) {
  client.request("transfer.received", {{"id", id}}, [this](QJsonValue v, QString e) {
    if (!e.isEmpty()) {
      issue(e);
      return;
    }
    QStringList urls;
    for (const auto &item : v.toObject()["items"].toArray()) {
      const auto path = item.toObject()["path"].toString();
      if (QDir::isAbsolutePath(path) && QFileInfo::exists(path))
        urls.append(QUrl::fromLocalFile(path).toString());
    }
    if (urls.isEmpty()) {
      const auto folder = v.toObject()["directory"].toString();
      if (QDir::isAbsolutePath(folder) && QFileInfo(folder).isDir())
        QDesktopServices::openUrl(QUrl::fromLocalFile(folder));
      else
        issue(text("Die Datei ist nicht mehr da. Vielleicht wurde sie verschoben oder gelöscht.", "The file is no longer there. It may have been moved or deleted."));
      return;
    }
    QDBusInterface fm("org.freedesktop.FileManager1", "/org/freedesktop/FileManager1", "org.freedesktop.FileManager1");
    if (fm.isValid())
      fm.asyncCall("ShowItems", urls, QString{});
    else
      QDesktopServices::openUrl(QUrl(urls.first()).adjusted(QUrl::RemoveFilename));
  });
}
void MainWindow::openReceiveFolder() {
  const auto folder = settings()["receiveDirectory"].toString();
  if (QDir::isAbsolutePath(folder) && QFileInfo(folder).isDir())
    QDesktopServices::openUrl(QUrl::fromLocalFile(folder));
  else
    issue(text("Der Ordner ist nicht mehr da. Wähle in den Einstellungen einen Empfangsordner.", "The folder is no longer there. Choose a receive folder in Settings."));
}
void MainWindow::chooseReceiveFolder() {
  const auto chosen = QFileDialog::getExistingDirectory(this, text("Wohin sollen empfangene Dateien?", "Where should received files go?"),
                                                        settings()["receiveDirectory"].toString(downloadsDirectory()));
  if (chosen.isEmpty())
    return;
  if (!client.ready()) {
    startupError.clear();
    client.shutdown([this, chosen](bool) {
      client.start(helperPath, qEnvironmentVariable("AIRLINK_DATA_DIR", dataDirectory()), false, chosen);
    });
    return;
  }
  updateSettings([chosen](QJsonObject &s) { s["receiveDirectory"] = chosen; });
}
void MainWindow::updateSettings(std::function<void(QJsonObject &)> change) {
  if (!client.ready())
    return;
  auto next = settings();
  change(next);
  client.request("settings.update", next, [this](QJsonValue, QString error) {
    if (!error.isEmpty())
      issue(error);
    client.refresh();
  });
}
void MainWindow::finishWelcome() {
  const auto name = welcomeName->text().trimmed();
  if (name.isEmpty() || name.toUtf8().size() > 120) {
    issue(text("Bitte einen Namen eingeben.", "Please enter a name."));
    return;
  }
  setAutostart(welcomeAutostart->isChecked());
  const auto folder = welcomePath;
  const bool notifications = welcomeNotify->isChecked();
  updateSettings([name, folder, notifications](QJsonObject &s) {
    s["name"] = name;
    if (QDir::isAbsolutePath(folder))
      s["receiveDirectory"] = folder;
    s["notifications"] = notifications;
    s["setupCompleted"] = true;
  });
}

bool MainWindow::autostartEnabled() const {
  const auto path = QDir(QStandardPaths::writableLocation(QStandardPaths::ConfigLocation)).filePath(QStringLiteral("autostart/") + autostartName);
  return QFileInfo::exists(path);
}
void MainWindow::setAutostart(bool enabled) {
  if (demoMode)
    return;
  const QDir config(QStandardPaths::writableLocation(QStandardPaths::ConfigLocation));
  const auto path = config.filePath(QStringLiteral("autostart/") + autostartName);
  if (!enabled) {
    QFile::remove(path);
    return;
  }
  config.mkpath("autostart");
  const auto appImage = qEnvironmentVariable("APPIMAGE");
  const auto program = !appImage.isEmpty() ? appImage : QCoreApplication::applicationFilePath();
  QSaveFile file(path);
  const QByteArray content = "[Desktop Entry]\nType=Application\nName=AirLink\nComment=Send files between your computers\nExec=" +
                             desktopQuoted(program).toUtf8() + " --background\nIcon=io.github.lucaskleinsteuber.AirLink\nTerminal=false\nNoDisplay=true\nX-GNOME-Autostart-enabled=true\nX-KDE-autostart-phase=2\n";
  if (!file.open(QIODevice::WriteOnly) || file.write(content) != content.size() || !file.commit())
    issue(text("„Beim Anmelden starten“ ließ sich nicht einschalten.", "Could not turn on Open at Login."));
}

void MainWindow::notify(const QString &key, const QString &title, const QString &body, const QString &transferId) {
  if (demoMode || !settings()["notifications"].toBool(true) || notified.contains(key))
    return;
  notified.insert(key);
  QDBusInterface notifications("org.freedesktop.Notifications", "/org/freedesktop/Notifications", "org.freedesktop.Notifications");
  if (!notifications.isValid()) {
    tray.showMessage(title, body);
    return;
  }
  QStringList actions;
  if (!transferId.isEmpty())
    actions << "reveal" << text("Zeigen", "Show");
  actions << "default" << text("Öffnen", "Open");
  QVariantMap hints{{"desktop-entry", "io.github.lucaskleinsteuber.AirLink"}};
  const auto call = notifications.asyncCall("Notify", "AirLink", uint(0), "io.github.lucaskleinsteuber.AirLink", title, body, actions, hints, int(-1));
  auto *watcher = new QDBusPendingCallWatcher(call, this);
  connect(watcher, &QDBusPendingCallWatcher::finished, this, [this, watcher, transferId] {
    QDBusPendingReply<uint> reply = *watcher;
    if (reply.isValid())
      notificationTransfers.insert(reply.value(), transferId);
    watcher->deleteLater();
  });
}
void MainWindow::notificationAction(uint id, const QString &actionKey) {
  if (!notificationTransfers.contains(id))
    return;
  const auto transfer = notificationTransfers.take(id);
  if (actionKey == "reveal" && !transfer.isEmpty())
    reveal(transfer);
  else
    activate();
}

void MainWindow::quit() {
  if (installingUpdate) {
    issue(text("Das geprüfte Update wird vorbereitet. Bitte kurz warten.", "The verified update is being prepared. Please wait."));
    return;
  }
  if (quitting)
    return;
  if (demoMode) {
    quitting = true;
    QApplication::quit();
    return;
  }
  int active = 0;
  for (const auto &v : client.status()["transfers"].toArray())
    if (!terminal(v.toObject()["state"].toString()))
      ++active;
  if (active > 0 &&
      QMessageBox::question(this, "AirLink", text("AirLink beenden? Laufende Übertragungen werden angehalten und beim nächsten Start fortgesetzt.",
                                                  "Quit AirLink? Running transfers pause and continue the next time AirLink starts.")) != QMessageBox::Yes)
    return;
  client.shutdown([this](bool stopped) {
    if (stopped) {
      quitting = true;
      tray.hide();
      QApplication::quit();
    } else {
      activate();
      issue(text("Der Dienst ließ sich nicht sauber beenden. Bitte noch einmal versuchen.", "The service could not stop cleanly. Please try again."));
    }
  });
}
void MainWindow::setUpdateMessage(const QString &message) {
  updateMessage = message;
  if (updateLabel)
    updateLabel->setText(message);
}
} // namespace AirLink
