#include "window.h"
#include "version.h"
#include <QApplication>
#include <QCheckBox>
#include <QClipboard>
#include <QCloseEvent>
#include <QComboBox>
#include <QDBusInterface>
#include <QDateTime>
#include <QDateTimeEdit>
#include <QDesktopServices>
#include <QDialog>
#include <QDialogButtonBox>
#include <QDir>
#include <QDragEnterEvent>
#include <QDropEvent>
#include <QFile>
#include <QFileDialog>
#include <QFileInfo>
#include <QFormLayout>
#include <QGroupBox>
#include <QHeaderView>
#include <QJsonDocument>
#include <QLabel>
#include <QLineEdit>
#include <QListWidget>
#include <QLocale>
#include <QMenu>
#include <QMenuBar>
#include <QMessageBox>
#include <QMimeData>
#include <QNetworkReply>
#include <QProgressBar>
#include <QPushButton>
#include <QSaveFile>
#include <QScrollArea>
#include <QSettings>
#include <QSignalBlocker>
#include <QSpinBox>
#include <QSplitter>
#include <QStackedWidget>
#include <QStandardPaths>
#include <QTableWidget>
#include <QTemporaryDir>
#include <QTextEdit>
#include <QTreeWidget>
#include <QUuid>
#include <QVBoxLayout>

namespace AirLink {
namespace {
class DropArea final : public QFrame {
public:
  std::function<void(QStringList)> dropped;
  explicit DropArea(QWidget *parent = nullptr) : QFrame(parent) {
    setAcceptDrops(true);
    setFrameShape(QFrame::StyledPanel);
    setMinimumHeight(170);
  }

protected:
  void dragEnterEvent(QDragEnterEvent *e) override {
    if (e->mimeData()->hasUrls())
      e->acceptProposedAction();
  }
  void dropEvent(QDropEvent *e) override {
    QStringList paths;
    for (const auto &u : e->mimeData()->urls())
      if (u.isLocalFile())
        paths.append(u.toLocalFile());
    if (!paths.isEmpty() && dropped) {
      dropped(paths);
      e->acceptProposedAction();
    }
  }
};
QPushButton *button(const QString &label, QWidget *parent,
                    std::function<void()> callback) {
  auto *b = new QPushButton(label, parent);
  QObject::connect(b, &QPushButton::clicked, parent,
                   [callback = std::move(callback)] { callback(); });
  return b;
}
QLabel *label(const QString &value, QWidget *parent, bool heading = false) {
  auto *v = new QLabel(value, parent);
  v->setWordWrap(true);
  v->setTextFormat(Qt::PlainText);
  if (heading) {
    auto f = v->font();
    f.setPointSizeF(f.pointSizeF() * 1.5);
    f.setBold(true);
    v->setFont(f);
  }
  return v;
}
QTableWidget *transferTable(QWidget *parent) {
  auto *t = new QTableWidget(0, 5, parent);
  t->setSelectionBehavior(QAbstractItemView::SelectRows);
  t->setEditTriggers(QAbstractItemView::NoEditTriggers);
  t->setShowGrid(false);
  t->setAlternatingRowColors(true);
  t->verticalHeader()->hide();
  t->horizontalHeader()->setSectionResizeMode(0, QHeaderView::Stretch);
  for (int i = 1; i < 5; ++i)
    t->horizontalHeader()->setSectionResizeMode(i,
                                                QHeaderView::ResizeToContents);
  t->setMinimumHeight(180);
  return t;
}
QString desktopQuoted(QString value) {
  value.replace("\\", "\\\\");
  value.replace('"', "\\\"");
  value.replace('`', "\\`");
  value.replace('$', "\\$");
  value.replace('%', "%%");
  return '"' + value + '"';
}
} // namespace

MainWindow::MainWindow(QWidget *parent)
    : QMainWindow(parent), client(this), tray(this), network(this),
      systemPalette(QApplication::palette()) {
  setWindowTitle("AirLink Beta");
  setWindowIcon(QIcon(":/airlink/icon.svg"));
  resize(1120, 760);
  setMinimumSize(860, 580);
  build();
  appliedEnglish = english();
  connect(&client, &DaemonClient::statusChanged, this, &MainWindow::render);
  connect(&client, &DaemonClient::failed, this, [this](const QString &e) {
    preparing = false;
    issue(e);
  });
  tray.setIcon(windowIcon());
  tray.setToolTip("AirLink");
  rebuildTrayMenu();
  connect(&tray, &QSystemTrayIcon::activated, this,
          [this](QSystemTrayIcon::ActivationReason r) {
            if (r == QSystemTrayIcon::Trigger)
              activate();
          });
  tray.show();
}
void MainWindow::rebuildTrayMenu() {
  auto *previous = tray.contextMenu();
  auto *menu = new QMenu(this);
  menu->addAction(text("AirLink öffnen", "Open AirLink"), this,
                  [this] { activate(); });
  menu->addAction(text("Dateien senden …", "Send files …"), this, [this] {
    activate();
    choose();
  });
  menu->addAction(text("Nach Updates suchen …", "Check for updates …"), this,
                  [this] { checkUpdates(); });
  menu->addSeparator();
  menu->addAction(text("Beenden", "Quit"), this, &MainWindow::quit);
  tray.setContextMenu(menu);
  if (previous)
    previous->deleteLater();
}
void MainWindow::applyPresentation() {
  const auto mode =
      client.status()["settings"].toObject()["appearance"].toString("system");
  if (mode != appliedAppearance) {
    auto palette = systemPalette;
    if (mode == "light" || mode == "dark") {
      const bool dark = mode == "dark";
      palette.setColor(QPalette::Window, QColor(dark ? "#252629" : "#F5F5F7"));
      palette.setColor(QPalette::Base, QColor(dark ? "#1B1C1F" : "#FFFFFF"));
      palette.setColor(QPalette::AlternateBase,
                       QColor(dark ? "#2D2E32" : "#ECEEF1"));
      palette.setColor(QPalette::Button, QColor(dark ? "#35363B" : "#F0F1F3"));
      palette.setColor(QPalette::ToolTipBase, palette.color(QPalette::Base));
      for (auto role : {QPalette::WindowText, QPalette::Text,
                        QPalette::ButtonText, QPalette::ToolTipText}) {
        palette.setColor(role, QColor(dark ? "#F2F2F4" : "#202124"));
        palette.setColor(QPalette::Disabled, role,
                         QColor(dark ? "#92949B" : "#777A82"));
      }
      palette.setColor(QPalette::Highlight,
                       QColor(dark ? "#3DAEE9" : "#007AFF"));
      palette.setColor(QPalette::HighlightedText, Qt::white);
    }
    appliedAppearance = mode;
    QApplication::setPalette(palette);
  }
  if (appliedEnglish != english()) {
    const auto page = pages->currentIndex(), pane = settingsPane;
    appliedEnglish = english();
    build();
    pages->setCurrentIndex(page);
    settingPages->setCurrentIndex(pane);
    rebuildTrayMenu();
  }
}
bool MainWindow::english() const {
  const auto lang =
      client.status()["settings"].toObject()["language"].toString("system");
  return lang == "en" ||
         (lang == "system" && QLocale::system().language() == QLocale::English);
}
QString MainWindow::text(const char *de, const char *en) const {
  return QString::fromUtf8(english() ? en : de);
}
void MainWindow::activate() {
  showNormal();
  raise();
  activateWindow();
  if (client.ready())
    render();
}
void MainWindow::closeEvent(QCloseEvent *e) {
  if (quitting)
    e->accept();
  else {
    hide();
    e->ignore();
  }
}
void MainWindow::start() {
  helperPath = QDir(QCoreApplication::applicationDirPath())
                   .absoluteFilePath("../libexec/airlink/airlink-service");
  if (!QFileInfo(helperPath).isExecutable()) {
    issue(text(
        "Der AirLink-Dienst fehlt. Installiere das vollständige Paket erneut.",
        "The AirLink service is missing. Reinstall the complete package."));
    return;
  }
  const bool fresh =
      !QFileInfo(QDir(dataDirectory()).filePath("settings.json")).exists();
  client.start(helperPath, dataDirectory(), false,
               fresh ? downloadsDirectory() : QString{});
}
void MainWindow::build() {
  auto *root = new QWidget(this);
  auto *outer = new QHBoxLayout(root);
  outer->setContentsMargins(0, 0, 0, 0);
  auto *split = new QSplitter(root);
  outer->addWidget(split);
  auto *side = new QWidget(split);
  auto *sl = new QVBoxLayout(side);
  sl->setContentsMargins(16, 20, 16, 16);
  sl->setSpacing(12);
  sl->addWidget(label(text("Geräte", "Devices"), side, true));
  devices = new QListWidget(side);
  devices->setWordWrap(true);
  devices->setSpacing(3);
  sl->addWidget(devices, 1);
  sl->addWidget(button(text("Gerät hinzufügen …", "Add device …"), side,
                       [this] { connectDevice(); }));
  sl->addWidget(button(text("Übertragungen", "Transfers"), side, [this] {
    pages->setCurrentIndex(1);
    loadHistory();
  }));
  auto *tools = new QPushButton(text("Weitere Werkzeuge", "More tools"), side);
  auto *toolsMenu = new QMenu(tools);
  toolsMenu->addAction("Quick Share", this, &MainWindow::shares);
  toolsMenu->addAction(text("Zwischenablage", "Clipboard"), this,
                       &MainWindow::clipboard);
  toolsMenu->addAction(text("Browser verbinden", "Connect a browser"), this,
                       &MainWindow::browserConnection);
  tools->setMenu(toolsMenu);
  sl->addWidget(tools);
  sl->addWidget(button(text("Einstellungen", "Settings"), side,
                       [this] { pages->setCurrentIndex(2); }));
  ownName = label("AirLink", side);
  ownName->setTextInteractionFlags(Qt::TextSelectableByMouse);
  sl->addWidget(ownName);
  sl->addWidget(label(QString::fromLatin1(Version), side));
  side->setMinimumWidth(240);
  side->setMaximumWidth(460);
  pages = new QStackedWidget(split);
  auto *send = new QWidget(pages);
  auto *layout = new QVBoxLayout(send);
  layout->setContentsMargins(28, 24, 28, 24);
  layout->setSpacing(16);
  auto *heading = new QHBoxLayout;
  title = label(text("Wähle ein Gerät", "Choose a device"), send, true);
  heading->addWidget(title, 1);
  heading->addWidget(button(text("Gerät verwalten", "Manage device"), send,
                            [this] { manageDevice(); }));
  layout->addLayout(heading);
  subtitle = label(text("Dateien bleiben in deinem Netzwerk.",
                        "Your files stay on your network."),
                   send);
  layout->addWidget(subtitle);
  attention = label("", send);
  attention->setTextFormat(Qt::RichText);
  connect(attention, &QLabel::linkActivated, this, [this] {
    const auto id = attention->property("pairId").toString();
    if (id == "setup")
      setup();
    else
      connectDevice(id);
  });
  layout->addWidget(attention);
  auto *drop = new DropArea(send);
  auto *dropLayout = new QVBoxLayout(drop);
  dropLayout->setContentsMargins(24, 22, 24, 22);
  dropLayout->addWidget(label(
      text("Dateien und Ordner senden", "Send files and folders"), drop, true));
  dropLayout->addWidget(label(text("Hier ablegen oder Dateien auswählen.",
                                   "Drop here or choose files."),
                              drop));
  auto *pickerRow = new QHBoxLayout;
  chooseButton = button(text("Dateien auswählen …", "Choose files …"), drop,
                        [this] { choose(); });
  pickerRow->addWidget(chooseButton);
  pickerRow->addWidget(button(text("Ordner auswählen …", "Choose folder …"),
                              drop, [this] { choose(true); }));
  pickerRow->addStretch();
  dropLayout->addLayout(pickerRow);
  drop->dropped = [this](const QStringList &paths) { acceptPaths(paths); };
  layout->addWidget(drop);
  prepared = label("", send);
  layout->addWidget(prepared);
  auto *actions = new QHBoxLayout;
  sendButton = button(text("Auswahl senden", "Send selection"), send,
                      [this] { sendPrepared(); });
  actions->addWidget(sendButton);
  actions->addWidget(
      button(text("Auswahl leeren", "Clear selection"), send, [this] {
        selection.clear();
        render();
      }));
  cancelPreparation = button(
      text("Vorbereitung abbrechen", "Cancel preparation"), send, [this] {
        if (!activePreparation.isEmpty())
          action("preparation.cancel", {{"id", activePreparation}});
      });
  actions->addWidget(cancelPreparation);
  actions->addStretch();
  layout->addLayout(actions);
  message = label("", send);
  message->setTextInteractionFlags(Qt::TextSelectableByMouse);
  layout->addWidget(message);
  retryButton = button(text("Erneut versuchen", "Retry"), send, [this] {
    if (!retryBatch.isEmpty())
      submit(retryBatch, submittedIdentities);
    else {
      client.shutdown([this](bool stopped) {
        if (stopped)
          start();
      });
    }
  });
  layout->addWidget(retryButton);
  retryButton->hide();
  currentTransfers = transferTable(send);
  layout->addWidget(currentTransfers, 1);
  pages->addWidget(send);
  auto *history = new QWidget(pages);
  auto *hl = new QVBoxLayout(history);
  hl->setContentsMargins(28, 24, 28, 24);
  hl->addWidget(label(text("Übertragungen", "Transfers"), history, true));
  hl->addWidget(label(
      text("Aktive Übertragungen und Verlauf. Entfernen löscht nur "
           "Listeneinträge.",
           "Active transfers and history. Removing entries keeps your files."),
      history));
  historyTransfers = transferTable(history);
  hl->addWidget(historyTransfers, 1);
  historyMore = button(text("Weitere 50 laden", "Load 50 more"), history,
                       [this] { loadHistory(true); });
  hl->addWidget(historyMore);
  hl->addWidget(button(text("Aktualisieren", "Refresh"), history,
                       [this] { loadHistory(); }));
  pages->addWidget(history);
  pages->addWidget(buildSettings());
  split->setSizes({290, 830});
  connect(devices, &QListWidget::currentItemChanged, this,
          [this](QListWidgetItem *item) {
            if (!item)
              return;
            selected = item->data(Qt::UserRole).toString();
            pages->setCurrentIndex(0);
            render();
          });
  menuBar()->clear();
  auto *file = menuBar()->addMenu(text("Datei", "File"));
  file->addAction(text("Dateien senden …", "Send files …"), QKeySequence::Open,
                  this, [this] { choose(); });
  file->addAction(text("Gerät hinzufügen …", "Add device …"), this,
                  [this] { connectDevice(); });
  file->addSeparator();
  auto *exit =
      file->addAction(text("Beenden", "Quit"), this, &MainWindow::quit);
  exit->setMenuRole(QAction::QuitRole);
  auto *app = menuBar()->addMenu("AirLink");
  app->addAction(text("Einstellungen", "Settings"), this,
                 [this] { pages->setCurrentIndex(2); });
  app->addAction(text("Nach Updates suchen …", "Check for updates …"), this,
                 [this] { checkUpdates(); });
  app->addAction(text("Empfangsordner öffnen", "Open receive folder"), this,
                 [this] {
                   const auto p = client.status()["settings"]
                                      .toObject()["receiveDirectory"]
                                      .toString();
                   if (QDir::isAbsolutePath(p))
                     QDesktopServices::openUrl(QUrl::fromLocalFile(p));
                 });
  auto *previous = takeCentralWidget();
  setCentralWidget(root);
  if (previous)
    previous->deleteLater();
}
QWidget *MainWindow::buildSettings() {
  auto *page = new QWidget(pages);
  auto *horizontal = new QHBoxLayout(page);
  auto *categories = new QListWidget(page);
  categories->addItems({text("Allgemein", "General"),
                        text("Empfangen", "Receiving"),
                        text("Übertragungen", "Transfers"),
                        text("Updates", "Updates"), text("Info", "About")});
  categories->setMaximumWidth(170);
  horizontal->addWidget(categories);
  auto *right = new QVBoxLayout;
  horizontal->addLayout(right, 1);
  settingPages = new QStackedWidget(page);
  right->addWidget(settingPages, 1);
  editors.clear();
  const auto formPage = [&](const QString &heading) {
    auto *w = new QWidget(settingPages);
    auto *l = new QVBoxLayout(w);
    l->addWidget(label(heading, w, true));
    auto *f = new QFormLayout;
    l->addLayout(f);
    l->addStretch();
    settingPages->addWidget(w);
    return f;
  };
  const auto field = [&](QFormLayout *form, const QString &key,
                         const QString &name) {
    auto *e = new QLineEdit(page);
    editors[key] = e;
    form->addRow(name, e);
    connect(e, &QLineEdit::textEdited, this,
            [this, key](const QString &value) { setDraftValue(key, value); });
    return e;
  };
  const auto toggle = [&](QFormLayout *form, const QString &key,
                          const QString &name) {
    auto *e = new QCheckBox(name, page);
    editors[key] = e;
    form->addRow(e);
    connect(e, &QCheckBox::toggled, this, [this, key](bool on) {
      if (!applyingEditors)
        setDraftValue(key, on);
    });
    return e;
  };
  auto *general = formPage(text("Allgemein", "General"));
  field(general, "name", text("Gerätename", "Device name"));
  for (const auto &key : QStringList{"appearance", "language"}) {
    auto *combo = new QComboBox(page);
    editors[key] = combo;
    if (key == "appearance") {
      combo->addItem(text("System", "System"), "system");
      combo->addItem(text("Hell", "Light"), "light");
      combo->addItem(text("Dunkel", "Dark"), "dark");
    } else {
      combo->addItem(text("System", "System"), "system");
      combo->addItem("Deutsch", "de");
      combo->addItem("English", "en");
    }
    general->addRow(key == "appearance" ? text("Darstellung", "Appearance")
                                        : text("Sprache", "Language"),
                    combo);
    connect(combo, qOverload<int>(&QComboBox::currentIndexChanged), this,
            [this, key, combo](int) {
              if (!applyingEditors)
                setDraftValue(key, combo->currentData().toString());
            });
  }
  toggle(general, "notifications",
         text("Bei Empfang und Anfragen benachrichtigen",
              "Notify about received files and requests"));
  general->addRow(button(
      text("Im Anwendungsmenü einrichten …", "Add to application menu …"), page,
      [this] { installAppImage(); }));
  auto *receive = formPage(text("Empfangen", "Receiving"));
  auto *folder =
      field(receive, "receiveDirectory", text("Zielordner", "Destination"));
  receive->addRow(button(
      text("Ordner auswählen …", "Choose folder …"), page, [this, folder] {
        const auto chosen = QFileDialog::getExistingDirectory(
            this, text("Empfangsordner", "Receive folder"), folder->text());
        if (!chosen.isEmpty())
          setDraftValue("receiveDirectory", chosen);
        applyEditors();
      }));
  receive->addRow(label(
      text("Dateien landen direkt hier. Bestehende Dateien werden nicht "
           "überschrieben.",
           "Files arrive directly here. Existing files are never overwritten."),
      page));
  receive->addRow(
      button(text("Einrichtung erneut öffnen …", "Open setup again …"), page,
             [this] { setup(); }));
  auto *advanced = new QWidget(page);
  auto *advancedLayout = new QVBoxLayout(advanced);
  advancedLayout->setContentsMargins(0, 0, 0, 0);
  auto *expand = new QPushButton(
      text("Erweiterte Dateiorganisation", "Advanced file organization"),
      advanced);
  expand->setCheckable(true);
  advancedLayout->addWidget(expand);
  auto *advancedContents = new QWidget(advanced);
  advancedContents->hide();
  advancedLayout->addWidget(advancedContents);
  connect(expand, &QPushButton::toggled, advancedContents,
          &QWidget::setVisible);
  auto *organization = new QVBoxLayout(advancedContents);
  for (const auto &key : QStringList{"detectDuplicates", "sortEnabled"}) {
    auto *check = new QCheckBox(
        key == "detectDuplicates"
            ? text("Duplikate erkennen", "Detect duplicates")
            : text("Sortierregeln anwenden", "Apply sorting rules"),
        advanced);
    editors[key] = check;
    organization->addWidget(check);
    connect(check, &QCheckBox::toggled, this, [this, key](bool value) {
      if (applyingEditors)
        return;
      auto o = draft.values["organization"].toObject();
      o[key] = value;
      setDraftValue("organization", o);
    });
  }
  auto *rules = new QTableWidget(0, 2, advanced);
  rules->setHorizontalHeaderLabels(
      {text("Dateiendungen", "Extensions"), text("Unterordner", "Subfolder")});
  rules->horizontalHeader()->setSectionResizeMode(QHeaderView::Stretch);
  rules->setMaximumHeight(150);
  editors["rules"] = rules;
  organization->addWidget(rules);
  organization->addWidget(label(
      text("Endungen mit Komma trennen. Die erste passende Regel gilt.",
           "Separate extensions with commas. The first matching rule applies."),
      advanced));
  auto *ruleActions = new QHBoxLayout;
  ruleActions->addWidget(
      button(text("Regel hinzufügen", "Add rule"), advanced, [this] {
        auto o = draft.values["organization"].toObject();
        auto rows = o["rules"].toArray();
        rows.append(
            QJsonObject{{"id", QUuid::createUuid().toString(QUuid::Id128)},
                        {"extensions", QJsonArray{"pdf"}},
                        {"folder", "Documents"}});
        o["rules"] = rows;
        setDraftValue("organization", o);
        applyEditors();
      }));
  ruleActions->addWidget(
      button(text("Regel entfernen", "Remove rule"), advanced, [this, rules] {
        const int selectedRow = rules->currentRow();
        auto o = draft.values["organization"].toObject();
        auto rows = o["rules"].toArray();
        if (selectedRow < 0 || selectedRow >= rows.size())
          return;
        rows.removeAt(selectedRow);
        o["rules"] = rows;
        setDraftValue("organization", o);
        rules->clearFocus();
        applyEditors();
      }));
  organization->addLayout(ruleActions);
  connect(rules, &QTableWidget::itemChanged, this,
          [this, rules](QTableWidgetItem *) {
            if (applyingEditors)
              return;
            QJsonArray rows;
            for (int i = 0; i < rules->rowCount(); ++i) {
              if (!rules->item(i, 0) || !rules->item(i, 1))
                return;
              QJsonArray extensions;
              for (const auto &e :
                   rules->item(i, 0)->text().split(',', Qt::SkipEmptyParts))
                extensions.append(e.trimmed());
              rows.append(QJsonObject{
                  {"id", rules->item(i, 0)->data(Qt::UserRole).toString()},
                  {"extensions", extensions},
                  {"folder", rules->item(i, 1)->text()}});
            }
            rules->setProperty("rules", rows);
            auto o = draft.values["organization"].toObject();
            o["rules"] = rows;
            setDraftValue("organization", o);
          });
  receive->addRow(advanced);
  auto *transfers = formPage(text("Übertragungen", "Transfers"));
  transfers->addRow(
      label(text("Ohne Limit wird die verfügbare Verbindung genutzt. 0 "
                 "bedeutet unbegrenzt.",
                 "Unlimited uses the available connection. 0 means unlimited."),
            page));
  for (const auto &key :
       QStringList{"sendLimitBytesPerSecond", "receiveLimitBytesPerSecond"}) {
    auto *edit = new QLineEdit(page);
    editors[key] = edit;
    transfers->addRow(key.startsWith("send")
                          ? text("Senden · MiB/s", "Send · MiB/s")
                          : text("Empfangen · MiB/s", "Receive · MiB/s"),
                      edit);
    connect(edit, &QLineEdit::textEdited, this,
            [this, key](const QString &raw) {
              bool ok = false;
              const double amount = QLocale().toDouble(raw, &ok);
              setDraftValue(key, ok ? QJsonValue(amount * 1048576)
                                    : QJsonValue(raw));
            });
  }
  auto *updates = formPage(text("Updates", "Updates"));
  toggle(updates, "automaticUpdateChecks",
         text("Über neue Betas informieren", "Notify about new betas"));
  updates->addRow(label(text("Optional, höchstens einmal pro 24 Stunden. "
                             "Installiert wird erst auf deinen Klick.",
                             "Optional, at most once every 24 hours. Updates "
                             "install only when you choose."),
                        page));
  updates->addRow(button(text("Nach Updates suchen …", "Check for updates …"),
                         page, [this] { checkUpdates(); }));
  updateFeedback = label("", page);
  updates->addRow(updateFeedback);
  auto *about = formPage(text("Info", "About"));
  about->addRow(label("AirLink " + QString::fromLatin1(Version), page));
  about->addRow(label("Copyright © 2026 Lucas Kleinsteuber · MIT", page));
  about->addRow(
      button(text("Lizenzen anzeigen", "Show licenses"), page, [this] {
        QFile license(":/airlink/LICENSE");
        if (!license.open(QIODevice::ReadOnly))
          return;
        QMessageBox::information(
            this, "AirLink · MIT",
            QString::fromUtf8(license.readAll()) +
                "\nQt: LGPLv3, dynamically linked. "
                "https://www.qt.io/licensing/open-source-lgpl-obligations");
      }));
  about->addRow(button("GitHub", page, [] {
    QDesktopServices::openUrl(
        QUrl("https://github.com/lucaskleinsteuber/Airlink"));
  }));
  settingsFeedback = label("", page);
  right->addWidget(settingsFeedback);
  auto *footer = new QHBoxLayout;
  footer->addStretch();
  discardButton = button(text("Verwerfen", "Discard"), page, [this] {
    draft.reset(client.status()["settings"].toObject());
    applyEditors();
  });
  saveButton =
      button(text("Speichern", "Save"), page, [this] { saveSettings(); });
  footer->addWidget(discardButton);
  footer->addWidget(saveButton);
  right->addLayout(footer);
  connect(categories, &QListWidget::currentRowChanged, this, [this](int index) {
    settingsPane = index;
    settingPages->setCurrentIndex(index);
  });
  categories->setCurrentRow(settingsPane);
  return page;
}
void MainWindow::setDraftValue(const QString &key, QJsonValue value) {
  draft.values[key] = value;
  saveButton->setEnabled(!saving && draft.dirty() &&
                         draft.validate().isEmpty());
  discardButton->setEnabled(!saving && draft.dirty());
  settingsFeedback->setText(
      draft.validate().isEmpty()
          ? text("Ungespeicherte Änderungen", "Unsaved changes")
          : text("Bitte prüfe deine Eingaben.", "Please check your entries."));
}
void MainWindow::applyEditors() {
  applyingEditors = true;
  for (auto it = editors.begin(); it != editors.end(); ++it) {
    auto value = draft.values[it.key()];
    if (it.key() == "detectDuplicates" || it.key() == "sortEnabled" ||
        it.key() == "rules")
      value = draft.values["organization"].toObject()[it.key()];
    if (auto *line = qobject_cast<QLineEdit *>(it.value())) {
      if (line->hasFocus() && line->isModified())
        continue;
      const QString s =
          it.key().endsWith("BytesPerSecond") && value.isDouble()
              ? QLocale().toString(value.toDouble() / 1048576, 'g', 12)
              : value.toString();
      if (line->text() != s)
        line->setText(s);
    } else if (auto *combo = qobject_cast<QComboBox *>(it.value()))
      combo->setCurrentIndex(std::max(0, combo->findData(value.toString())));
    else if (auto *check = qobject_cast<QCheckBox *>(it.value()))
      check->setChecked(value.toBool());
    else if (auto *rules = qobject_cast<QTableWidget *>(it.value())) {
      const auto rows = value.toArray();
      if (rules->property("rules").toJsonArray() == rows)
        continue;
      rules->setRowCount(0);
      for (const auto &v : rows) {
        const auto r = v.toObject();
        const int row = rules->rowCount();
        rules->insertRow(row);
        QStringList extensions;
        for (const auto &e : r["extensions"].toArray())
          extensions.append(e.toString());
        auto *first = new QTableWidgetItem(extensions.join(", "));
        first->setData(Qt::UserRole, r["id"]);
        rules->setItem(row, 0, first);
        rules->setItem(row, 1, new QTableWidgetItem(r["folder"].toString()));
      }
      rules->setProperty("rules", rows);
    }
  }
  applyingEditors = false;
  saveButton->setEnabled(client.ready() && !saving && draft.dirty() &&
                         draft.validate().isEmpty());
  discardButton->setEnabled(!saving && draft.dirty());
  settingsFeedback->setText(
      draft.dirty() ? text("Ungespeicherte Änderungen", "Unsaved changes")
                    : text("Gespeichert", "Saved"));
}
void MainWindow::saveSettings() {
  if (saving || !draft.validate().isEmpty())
    return;
  saving = true;
  applyEditors();
  const auto merged = draft.merged(client.status()["settings"].toObject());
  client.request("settings.update", merged,
                 [this](QJsonValue value, QString e) {
                   saving = false;
                   if (!e.isEmpty())
                     issue(e);
                   else {
                     draft.reset(value.toObject());
                     settingsFeedback->setText(text("Gespeichert", "Saved"));
                     client.refresh();
                   }
                   applyEditors();
                 });
}
QJsonObject MainWindow::selectedDevice() const {
  for (const auto &v : client.status()["devices"].toArray())
    if (v.toObject()["id"].toString() == selected)
      return v.toObject();
  return {};
}
void MainWindow::render() {
  const auto status = client.status(), settings = status["settings"].toObject();
  if (!settingsLoaded) {
    draft.reset(settings);
    settingsLoaded = true;
  } else if (draft.baseline != settings && !saving)
    draft.rebase(settings);
  applyPresentation();
  applyEditors();
  const auto listed = sortedDevices(status["devices"].toArray(), true);
  QJsonArray previous;
  for (int i = 0; i < devices->count(); ++i)
    previous.append(devices->item(i)->data(Qt::UserRole + 1).toJsonObject());
  if (listed != previous) {
    QSignalBlocker blocked(devices);
    devices->clear();
    for (const auto &v : listed) {
      const auto d = v.toObject();
      auto *item = new QListWidgetItem(
          QIcon::fromTheme("computer"),
          d["name"].toString() + "\n" +
              (d["online"].toBool() ? text("Erreichbar", "Available")
                                    : text("Nicht erreichbar", "Unavailable")),
          devices);
      item->setData(Qt::UserRole, d["id"]);
      item->setData(Qt::UserRole + 1, d);
      item->setToolTip(d["name"].toString());
      item->setSizeHint(QSize(220, 58));
      if (d["id"].toString() == selected)
        devices->setCurrentItem(item);
    }
  }
  ownName->setText(status["name"].toString());
  const auto device = selectedDevice();
  title->setText(device.isEmpty() ? text("Wähle ein Gerät", "Choose a device")
                                  : device["name"].toString());
  subtitle->setText(
      device.isEmpty()
          ? text("Füge deinen anderen Computer einmal hinzu, um Dateien zu "
                 "senden.",
                 "Add your other computer once to start sending files.")
      : device["online"].toBool()
          ? text("Bereit zum Senden im lokalen Netzwerk",
                 "Ready to send on your local network")
          : text("Offline. Sendungen warten auf die Verbindung.",
                 "Offline. Sends will wait for a connection."));
  prepared->setText(selection.empty() ? QString{}
                                      : text("Vorbereitete Auswahl: ",
                                             "Prepared selection: ") +
                                            selection.paths().join(", "));
  prepared->setVisible(!selection.empty());
  sendButton->setVisible(!selection.empty());
  sendButton->setEnabled(client.ready() && !preparing &&
                         device["canReceive"].toBool());
  chooseButton->setEnabled(client.ready() && !preparing);
  cancelPreparation->setVisible(preparing && !activePreparation.isEmpty());
  if (!activePreparation.isEmpty())
    for (const auto &v : status["preparations"].toArray()) {
      const auto p = v.toObject();
      if (p["id"].toString() != activePreparation)
        continue;
      const auto state = p["state"].toString();
      if (state != "preparing") {
        preparing = false;
        activePreparation.clear();
        if (state == "completed") {
          selection.acknowledge(submittedIdentities);
          retryBatch = {};
          retryButton->hide();
          message->setText(text("Sendung in der Warteschlange.",
                                "Send added to the queue."));
        } else
          issue(p["error"].toString(
              text("Vorbereitung abgebrochen. Die Auswahl bleibt erhalten.",
                   "Preparation cancelled. Your selection is kept.")));
      }
    }
  if (isVisible() && pages->currentIndex() == 0)
    renderTransfers(
        currentTransfers,
        selected.isEmpty()
            ? QJsonArray{}
            : visibleTransfers(status["transfers"].toArray(), selected));
  historyRows = mergeHistory(historyRows, status["transfers"].toArray());
  if (isVisible() && pages->currentIndex() == 1)
    renderTransfers(historyTransfers, historyRows);
  attention->hide();
  if (!settings["setupCompleted"].toBool()) {
    attention->setText("<a href=\"setup\">" +
                       text("AirLink einrichten", "Set up AirLink") + "</a>");
    attention->setProperty("pairId", "setup");
    attention->show();
  }
  for (const auto &v : status["pairingSessions"].toArray()) {
    const auto p = v.toObject();
    if (p["state"] == "comparing" && !p["localConfirmed"].toBool()) {
      attention->setText(
          "<a href=\"pair\">" +
          (text("Verbindung mit ", "Connect to ") + p["peerName"].toString())
              .toHtmlEscaped() +
          "</a>");
      attention->setProperty("pairId", p["id"]);
      attention->show();
      if (initialized && settings["notifications"].toBool() &&
          !seenPairs.contains(p["id"].toString()))
        tray.showMessage("AirLink",
                         text("Verbindungsanfrage: ", "Pairing request: ") +
                             p["peerName"].toString());
      seenPairs.insert(p["id"].toString());
    }
  }
  for (const auto &v : status["transfers"].toArray()) {
    const auto t = v.toObject();
    if (t["state"] == "completed") {
      if (initialized && settings["notifications"].toBool() &&
          !seenCompletions.contains(t["id"].toString()))
        tray.showMessage("AirLink", text("Übertragung abgeschlossen: ",
                                         "Transfer completed: ") +
                                        transferTitle(t));
      seenCompletions.insert(t["id"].toString());
    }
  }
  if (!initialized) {
    initialized = true;
    if (!settings["setupCompleted"].toBool())
      QTimer::singleShot(0, this, [this] { setup(); });
  }
  maybeCheckUpdates();
}
void MainWindow::renderTransfers(QTableWidget *table, const QJsonArray &rows) {
  table->setHorizontalHeaderLabels(
      {text("Datei", "File"), text("Status", "State"),
       text("Fortschritt", "Progress"),
       text("Tempo / Restzeit", "Speed / remaining"),
       text("Aktion", "Action")});
  QSet<QString> ids;
  for (const auto &v : rows)
    ids.insert(v.toObject()["id"].toString());
  for (int i = table->rowCount() - 1; i >= 0; --i)
    if (!ids.contains(table->item(i, 0)->data(Qt::UserRole).toString()))
      table->removeRow(i);
  for (const auto &v : rows) {
    const auto t = v.toObject();
    const auto id = t["id"].toString();
    int row = -1;
    for (int i = 0; i < table->rowCount(); ++i)
      if (table->item(i, 0)->data(Qt::UserRole).toString() == id) {
        row = i;
        break;
      }
    if (row < 0) {
      row = table->rowCount();
      table->insertRow(row);
      for (int col = 0; col < 4; ++col)
        table->setItem(row, col, new QTableWidgetItem);
      table->item(row, 0)->setData(Qt::UserRole, id);
    }
    table->item(row, 0)->setText(transferTitle(t) + "\n" +
                                 t["deviceName"].toString());
    table->item(row, 0)->setToolTip(transferTitle(t));
    table->item(row, 1)->setText(stateLabel(t["state"].toString(), english()));
    const double total = t["totalBytes"].toDouble(),
                 done = t["completedBytes"].toDouble() +
                        t["skippedBytes"].toDouble();
    const int percent =
        t["state"] == "completed" ? 100
        : total > 0
            ? static_cast<int>(std::clamp(done / total * 100, 0.0, 100.0))
            : 0;
    table->item(row, 2)->setText(QString::number(percent) + "%\n" +
                                 bytes(t["completedBytes"].toInteger()));
    table->item(row, 3)->setText(speed(t["bytesPerSecond"].toDouble()) + "\n" +
                                 remaining(t));
    const auto previous =
        table->item(row, 0)->data(Qt::UserRole + 1).toJsonObject();
    table->item(row, 0)->setData(Qt::UserRole + 1, t);
    if (previous.isEmpty() || previous["state"] != t["state"] ||
        previous["direction"] != t["direction"] ||
        previous["hasContent"] != t["hasContent"]) {
      auto *actions = new QWidget(table);
      auto *layout = new QHBoxLayout(actions);
      layout->setContentsMargins(2, 0, 2, 0);
      if (t["direction"] == "receive" && t["state"] == "offered")
        layout->addWidget(
            button(text("Annehmen", "Accept"), actions,
                   [this, id] { action("transfer.accept", {{"id", id}}); }));
      else if (t["direction"] == "receive" && t["state"] == "completed" &&
               t["hasContent"].toBool())
        layout->addWidget(button(text("Zeigen", "Show"), actions,
                                 [this, id] { reveal(id); }));
      layout->addWidget(button("⋯", actions, [this, table, id] {
        for (int i = 0; i < table->rowCount(); ++i)
          if (table->item(i, 0)->data(Qt::UserRole).toString() == id) {
            transferMenu(
                table->item(i, 0)->data(Qt::UserRole + 1).toJsonObject());
            break;
          }
      }));
      table->setCellWidget(row, 4, actions);
    }
    table->setRowHeight(row, 60);
  }
}
void MainWindow::issue(const QString &error) {
  message->setText(error);
  message->show();
  retryButton->setVisible(!retryBatch.isEmpty() || !client.ready());
}
void MainWindow::action(const QString &method, const QJsonObject &params) {
  client.request(method, params, [this, method](QJsonValue, QString e) {
    if (!e.isEmpty())
      issue(e);
    client.refresh();
    if (method == "history.remove")
      loadHistory();
  });
}
void MainWindow::choose(bool folders) {
  if (!client.status()["settings"].toObject()["setupCompleted"].toBool()) {
    setup();
    return;
  }
  if (preparing)
    return;
  const auto recipient = selectedDevice();
  const QStringList paths =
      folders ? QStringList{QFileDialog::getExistingDirectory(
                    this, text("Ordner senden", "Send folder"))}
              : QFileDialog::getOpenFileNames(
                    this, text("Dateien senden", "Send files"));
  QStringList valid;
  for (const auto &p : paths)
    if (!p.isEmpty())
      valid.append(p);
  if (valid.isEmpty())
    return;
  const auto batch = sendBatch(recipient, valid);
  if (!batch.isEmpty())
    submit(batch);
  else {
    selection.append(valid);
    render();
  }
}
void MainWindow::acceptPaths(const QStringList &paths) {
  if (paths.isEmpty())
    return;
  const auto batch = sendBatch(selectedDevice(), paths);
  if (client.ready() && !preparing &&
      client.status()["settings"].toObject()["setupCompleted"].toBool() &&
      !batch.isEmpty())
    submit(batch);
  else {
    selection.append(paths);
    if (initialized)
      render();
  }
}
void MainWindow::sendPrepared() {
  const auto batch = sendBatch(selectedDevice(), selection.paths());
  if (!batch.isEmpty())
    submit(batch, selection.identities());
}
void MainWindow::submit(QJsonObject batch, QStringList identities) {
  if (preparing)
    return;
  preparing = true;
  retryBatch = batch;
  submittedIdentities = identities;
  message->setText(text("Sendung wird vorbereitet …", "Preparing send …"));
  retryButton->hide();
  client.request("preparation.start", batch, [this](QJsonValue v, QString e) {
    if (!e.isEmpty()) {
      preparing = false;
      issue(e);
    } else
      activePreparation = v.toObject()["id"].toString();
    client.refresh();
  });
}
void MainWindow::loadHistory(bool more) {
  if (historyLoading)
    return;
  historyLoading = true;
  const auto generation = ++historyGeneration;
  QJsonObject query{{"limit", 50}};
  if (more)
    query["cursor"] = historyCursor;
  client.request(
      "history.page", query, [this, generation, more](QJsonValue v, QString e) {
        if (generation != historyGeneration)
          return;
        historyLoading = false;
        if (!e.isEmpty()) {
          issue(e);
          return;
        }
        if (!more)
          historyRows = {};
        for (const auto &row : v.toObject()["transfers"].toArray())
          historyRows.append(row);
        historyCursor = v.toObject()["nextCursor"].toString();
        historyMore->setEnabled(!historyCursor.isEmpty());
        historyRows =
            mergeHistory(historyRows, client.status()["transfers"].toArray());
        if (isVisible())
          renderTransfers(historyTransfers, historyRows);
      });
}
void MainWindow::transferMenu(const QJsonObject &t) {
  const auto id = t["id"].toString(), state = t["state"].toString();
  QMenu menu(this);
  menu.addAction(text("Details", "Details"), this,
                 [this, t] { transferDetails(t); });
  if (state == "offered" && t["direction"] == "receive")
    menu.addAction(
        text("Annehmen und künftig automatisch empfangen",
             "Accept and automatically receive in future"),
        this, [this, id] { action("transfer.acceptTrusted", {{"id", id}}); });
  if (!terminal(state)) {
    if (state == "paused" || state == "reconnecting")
      menu.addAction(text("Fortsetzen", "Resume"), this,
                     [this, id] { action("transfer.resume", {{"id", id}}); });
    else
      menu.addAction(text("Pausieren", "Pause"), this,
                     [this, id] { action("transfer.pause", {{"id", id}}); });
    menu.addAction(text("Abbrechen", "Cancel"), this,
                   [this, id] { action("transfer.cancel", {{"id", id}}); });
  } else
    menu.addAction(text("Aus Liste entfernen", "Remove from list"), this,
                   [this, id] { action("history.remove", {{"id", id}}); });
  menu.exec(QCursor::pos());
}
void MainWindow::transferDetails(const QJsonObject &t) {
  auto *dialog = new QDialog(this);
  dialog->setAttribute(Qt::WA_DeleteOnClose);
  dialog->setWindowTitle(transferTitle(t));
  dialog->resize(720, 480);
  auto *layout = new QVBoxLayout(dialog);
  layout->addWidget(label(transferTitle(t), dialog, true));
  layout->addWidget(label(t["error"].toString(), dialog));
  auto *files = new QTreeWidget(dialog);
  files->setHeaderLabels(
      {text("Datei", "File"), text("Größe", "Size"), text("Status", "State")});
  files->header()->setSectionResizeMode(0, QHeaderView::Stretch);
  layout->addWidget(files, 1);
  auto *more = button(text("Weitere Dateien laden", "Load more files"), dialog,
                      [this, dialog, files] {
                        loadFilePage(dialog, files,
                                     dialog->findChild<QPushButton *>("more"),
                                     dialog->property("transferId").toString(),
                                     dialog->property("offset").toInt());
                      });
  more->setObjectName("more");
  dialog->setProperty("transferId", t["id"]);
  layout->addWidget(more);
  auto *decision = new QHBoxLayout;
  for (const auto &choice : QStringList{"keep", "skip"})
    decision->addWidget(button(
        choice == "keep"
            ? text("Ausgewähltes Duplikat behalten", "Keep selected duplicate")
            : text("Ausgewähltes Duplikat überspringen",
                   "Skip selected duplicate"),
        dialog, [this, files, t, choice] {
          const auto *item = files->currentItem();
          if (!item)
            return;
          const auto f = item->data(0, Qt::UserRole).toJsonObject();
          if (f["duplicateState"] != "pending")
            return;
          QJsonObject params{
              {"transferId", t["id"]}, {"fileId", f["id"]}, {"action", choice}};
          if (choice == "skip") {
            const auto candidates = f["duplicateCandidates"].toArray();
            if (candidates.isEmpty())
              return;
            params["candidateId"] = candidates.first().toObject()["id"];
          }
          action("duplicate.resolve", params);
        }));
  layout->addLayout(decision);
  layout->addWidget(
      button(text("Fertig", "Done"), dialog, [dialog] { dialog->close(); }));
  loadFilePage(dialog, files, more, t["id"].toString(), 0);
  dialog->show();
}
void MainWindow::loadFilePage(QPointer<QDialog> dialog, QTreeWidget *files,
                              QPushButton *more, const QString &id,
                              int offset) {
  more->setEnabled(false);
  client.request(
      "transfer.files", {{"id", id}, {"offset", offset}, {"limit", 50}},
      [this, dialog, files, more](QJsonValue v, QString e) {
        if (!dialog)
          return;
        if (!e.isEmpty()) {
          issue(e);
          return;
        }
        for (const auto &value : v.toObject()["files"].toArray()) {
          const auto f = value.toObject();
          auto *row = new QTreeWidgetItem(
              files, {f["path"].toString(), bytes(f["size"].toInteger()),
                      f["duplicateState"].toString()});
          row->setData(0, Qt::UserRole, f);
        }
        const int next = v.toObject()["nextOffset"].toInt(-1);
        dialog->setProperty("offset", next);
        more->setEnabled(next >= 0);
      });
}
void MainWindow::reveal(const QString &id) {
  client.request(
      "transfer.received", {{"id", id}}, [this](QJsonValue v, QString e) {
        if (!e.isEmpty()) {
          issue(e);
          return;
        }
        const auto items = v.toObject()["items"].toArray();
        if (items.isEmpty()) {
          issue(text("Die empfangene Datei ist nicht mehr vorhanden.",
                     "The received file is no longer available."));
          return;
        }
        const auto path = items.first().toObject()["path"].toString();
        if (!QDir::isAbsolutePath(path))
          return;
        QDBusInterface fm("org.freedesktop.FileManager1",
                          "/org/freedesktop/FileManager1",
                          "org.freedesktop.FileManager1");
        if (fm.isValid())
          fm.asyncCall("ShowItems",
                       QStringList{QUrl::fromLocalFile(path).toString()},
                       QString{});
        else
          QDesktopServices::openUrl(QUrl::fromLocalFile(
              QFileInfo(path).isDir() ? path : QFileInfo(path).absolutePath()));
      });
}

void MainWindow::setup() {
  if (!client.ready())
    return;
  auto *dialog = new QDialog(this);
  dialog->setAttribute(Qt::WA_DeleteOnClose);
  dialog->setWindowTitle(text("AirLink einrichten", "Set up AirLink"));
  dialog->resize(560, 420);
  QPointer<QDialog> guard = dialog;
  auto *layout = new QVBoxLayout(dialog);
  layout->setSpacing(18);
  auto *heading =
      label(text("Wie heißt dieser Computer?", "What is this computer called?"),
            dialog, true);
  layout->addWidget(heading);
  auto *stack = new QStackedWidget(dialog);
  layout->addWidget(stack, 1);
  const auto settings = client.status()["settings"].toObject();
  auto *first = new QWidget(stack);
  auto *firstLayout = new QVBoxLayout(first);
  firstLayout->addWidget(label(
      text("Mit diesem Namen erkennen deine anderen Geräte diesen Computer.",
           "Your other devices will recognize this computer by this name."),
      first));
  auto *name = new QLineEdit(settings["name"].toString(), first);
  firstLayout->addWidget(name);
  firstLayout->addStretch();
  stack->addWidget(first);
  auto *second = new QWidget(stack);
  auto *secondLayout = new QVBoxLayout(second);
  secondLayout->addWidget(
      label(text("Empfangene Dateien landen direkt im gewählten Ordner.",
                 "Received files arrive directly in the selected folder."),
            second));
  auto *folder = new QLineEdit(
      settings["receiveDirectory"].toString(downloadsDirectory()), second);
  secondLayout->addWidget(folder);
  secondLayout->addWidget(button(
      text("Ordner auswählen …", "Choose folder …"), second, [this, folder] {
        const auto p = QFileDialog::getExistingDirectory(
            this, text("Empfangsordner", "Receive folder"), folder->text());
        if (!p.isEmpty())
          folder->setText(p);
      }));
  secondLayout->addStretch();
  stack->addWidget(second);
  auto *third = new QWidget(stack);
  auto *thirdLayout = new QVBoxLayout(third);
  auto *review = label("", third);
  thirdLayout->addWidget(review);
  auto *notifications =
      new QCheckBox(text("Bei Empfang und Anfragen benachrichtigen",
                         "Notify about received files and requests"),
                    third);
  notifications->setChecked(settings["notifications"].toBool());
  thirdLayout->addWidget(notifications);
  auto *updates = new QCheckBox(
      text("Über neue Betas informieren", "Notify about new betas"), third);
  updates->setChecked(settings["automaticUpdateChecks"].toBool());
  thirdLayout->addWidget(updates);
  auto *connectNext = new QCheckBox(
      text("Danach ein Gerät hinzufügen", "Add a device next"), third);
  connectNext->setChecked(
      sortedDevices(client.status()["devices"].toArray(), true).isEmpty());
  thirdLayout->addWidget(connectNext);
  thirdLayout->addStretch();
  stack->addWidget(third);
  auto *feedback = label("", dialog);
  layout->addWidget(feedback);
  auto *footer = new QHBoxLayout;
  auto *back = button(text("Zurück", "Back"), dialog, [stack, heading, this] {
    stack->setCurrentIndex(std::max(0, stack->currentIndex() - 1));
    heading->setText(
        stack->currentIndex() == 0
            ? text("Wie heißt dieser Computer?",
                   "What is this computer called?")
            : text("Wohin sollen Dateien?", "Where should files arrive?"));
  });
  footer->addWidget(back);
  footer->addWidget(
      button(text("Später", "Later"), dialog, [dialog] { dialog->close(); }));
  footer->addStretch();
  auto *next = new QPushButton(text("Weiter", "Continue"), dialog);
  next->setDefault(true);
  footer->addWidget(next);
  layout->addLayout(footer);
  connect(
      next, &QPushButton::clicked, dialog,
      [this, guard, stack, name, folder, review, heading, next, back, feedback,
       notifications, updates, connectNext] {
        SettingsDraft validation;
        validation.reset(client.status()["settings"].toObject());
        validation.values["name"] = name->text().trimmed();
        validation.values["receiveDirectory"] = folder->text();
        if (!validation.validate().isEmpty()) {
          feedback->setText(
              text("Bitte gib einen Namen und einen gültigen Zielordner an.",
                   "Enter a name and a valid destination folder."));
          return;
        }
        if (stack->currentIndex() < 2) {
          stack->setCurrentIndex(stack->currentIndex() + 1);
          heading->setText(
              stack->currentIndex() == 1
                  ? text("Wohin sollen Dateien?", "Where should files arrive?")
                  : text("Bereit zum Teilen", "Ready to share"));
          review->setText(name->text().trimmed() + "\n" + folder->text());
          next->setText(stack->currentIndex() == 2
                            ? text("Einrichtung abschließen", "Finish setup")
                            : text("Weiter", "Continue"));
          return;
        }
        auto value = validation.values;
        value["notifications"] = notifications->isChecked();
        value["automaticUpdateChecks"] = updates->isChecked();
        value["setupCompleted"] = true;
        next->setEnabled(false);
        back->setEnabled(false);
        const bool addNext = connectNext->isChecked();
        client.request("settings.update", value,
                       [this, guard, next, back, feedback, addNext](QJsonValue,
                                                                    QString e) {
                         if (!guard)
                           return;
                         if (!e.isEmpty()) {
                           feedback->setText(e);
                           next->setEnabled(true);
                           back->setEnabled(true);
                           return;
                         }
                         guard->close();
                         client.refresh();
                         if (addNext)
                           QTimer::singleShot(0, this,
                                              [this] { connectDevice(); });
                       });
      });
  dialog->show();
  name->setFocus();
}

void MainWindow::connectDevice(const QString &session) {
  if (!client.ready())
    return;
  if (pairingDialog) {
    pairingDialog->raise();
    pairingDialog->activateWindow();
    return;
  }
  auto *dialog = new QDialog(this);
  dialog->setAttribute(Qt::WA_DeleteOnClose);
  pairingDialog = dialog;
  QPointer<QDialog> guard = dialog;
  dialog->setWindowTitle(text("Gerät hinzufügen", "Add a device"));
  dialog->resize(580, 520);
  dialog->setProperty("session", session);
  auto *layout = new QVBoxLayout(dialog);
  layout->addWidget(
      label(text("Gerät hinzufügen", "Add a device"), dialog, true));
  auto *stack = new QStackedWidget(dialog);
  layout->addWidget(stack, 1);
  auto *selectionPage = new QWidget(stack);
  auto *selectLayout = new QVBoxLayout(selectionPage);
  selectLayout->addWidget(label(
      text("Öffne AirLink auf beiden Computern und wähle das andere Gerät.",
           "Open AirLink on both computers and choose the other device."),
      selectionPage));
  auto *nearby = new QListWidget(selectionPage);
  selectLayout->addWidget(nearby, 1);
  auto *legacyRequests = new QListWidget(selectionPage);
  legacyRequests->setMaximumHeight(130);
  selectLayout->addWidget(legacyRequests);
  selectLayout->addWidget(button(
      text("Andere Verbindungsmöglichkeiten …", "Other connection options …"),
      selectionPage, [this, guard] {
        if (!guard)
          return;
        auto *manual = new QDialog(guard);
        manual->setAttribute(Qt::WA_DeleteOnClose);
        manual->setWindowTitle(
            text("Mit Fingerabdruck verbinden", "Connect with fingerprint"));
        auto *l = new QFormLayout(manual);
        auto *address = new QLineEdit(manual), *fp = new QLineEdit(manual);
        l->addRow(text("HTTPS-Adresse", "HTTPS address"), address);
        l->addRow(text("Vollständiger Fingerabdruck", "Complete fingerprint"),
                  fp);
        auto *verified = new QCheckBox(
            text("Ich habe den Fingerabdruck auf dem anderen Gerät verglichen.",
                 "I compared the fingerprint on the other device."),
            manual);
        l->addRow(verified);
        auto *approve = button(
            text("Verbindung anfragen", "Request connection"), manual,
            [this, manual, address, fp, verified] {
              if (!verified->isChecked())
                return;
              action("peer.connect", {{"url", address->text().trimmed()},
                                      {"fingerprint", fp->text().trimmed()}});
              manual->close();
            });
        approve->setEnabled(false);
        connect(verified, &QCheckBox::toggled, approve,
                &QPushButton::setEnabled);
        l->addRow(approve);
        manual->show();
      }));
  stack->addWidget(selectionPage);
  auto *comparison = new QWidget(stack);
  auto *cl = new QVBoxLayout(comparison);
  auto *peer = label("", comparison, true);
  cl->addWidget(peer);
  cl->addWidget(label(text("Vergleiche diesen Code auf beiden Geräten.",
                           "Compare this code on both devices."),
                      comparison));
  auto *code = label("", comparison, true);
  auto font = code->font();
  font.setPointSize(32);
  font.setStyleHint(QFont::Monospace);
  code->setFont(font);
  code->setAlignment(Qt::AlignCenter);
  cl->addWidget(code);
  auto *progress = label("", comparison);
  cl->addWidget(progress);
  auto *automatic = new QCheckBox(
      text("Dateien dieses Geräts künftig automatisch annehmen",
           "Automatically receive files from this device in future"),
      comparison);
  cl->addWidget(automatic);
  cl->addWidget(
      label(text("Optional. Gilt nur für den Empfang auf diesem Computer.",
                 "Optional. Applies only to receiving on this computer."),
            comparison));
  auto *confirm =
      button(text("Code stimmt überein", "Code matches"), comparison,
             [this, guard, automatic] {
               if (!guard)
                 return;
               client.request("pairing.confirm",
                              {{"id", guard->property("session").toString()},
                               {"automaticReceive", automatic->isChecked()}},
                              [this, guard](QJsonValue, QString e) {
                                if (!guard)
                                  return;
                                if (!e.isEmpty())
                                  issue(e);
                                client.refresh();
                              });
             });
  cl->addWidget(confirm);
  cl->addStretch();
  stack->addWidget(comparison);
  auto *footer = new QHBoxLayout;
  footer->addWidget(button(text("Abbrechen", "Cancel"), dialog, [this, guard] {
    if (!guard)
      return;
    const auto id = guard->property("session").toString();
    if (!id.isEmpty())
      action("pairing.cancel", {{"id", id}});
    guard->close();
  }));
  footer->addStretch();
  footer->addWidget(
      button(text("Fertig", "Done"), dialog, [dialog] { dialog->close(); }));
  layout->addLayout(footer);
  const auto update = [this, guard, nearby, legacyRequests, stack, peer, code,
                       progress, automatic, confirm] {
    if (!guard)
      return;
    const auto current = client.status();
    const auto id = guard->property("session").toString();
    for (const auto &v : current["pairingSessions"].toArray()) {
      const auto p = v.toObject();
      if (p["id"].toString() != id)
        continue;
      stack->setCurrentIndex(1);
      peer->setText(p["peerName"].toString());
      const auto value = p["comparisonCode"].toString();
      code->setText(value.size() == 8 ? value.left(4) + " " + value.mid(4)
                                      : QString{});
      confirm->setEnabled(p["state"] == "comparing" &&
                          !p["localConfirmed"].toBool());
      automatic->setEnabled(!p["localConfirmed"].toBool());
      const auto state = p["state"].toString();
      progress->setText(
          state == "paired" ? text("Verbunden. Du kannst jetzt Dateien senden.",
                                   "Connected. You can now send files.")
          : state == "starting"
              ? text("Verbindung wird vorbereitet …", "Preparing connection …")
          : state == "finishing"
              ? text("Verbindung wird gespeichert …", "Saving connection …")
          : state == "comparing"
              ? (p["localConfirmed"].toBool()
                     ? text("Warte auf die Bestätigung am anderen Gerät.",
                            "Waiting for confirmation on the other device.")
                     : text("Der Code ist fünf Minuten gültig.",
                            "The code is valid for five minutes."))
              : p["error"].toString(text(
                    "Verbindung abgelaufen oder abgebrochen. Starte eine neue "
                    "Anfrage.",
                    "Connection expired or cancelled. Start a new request.")));
      if (state == "paired") {
        selected = p["peerId"].toString();
        render();
      }
      return;
    }
    stack->setCurrentIndex(0);
    const auto peers = sortedDevices(current["devices"].toArray(), false);
    QSignalBlocker block(nearby);
    nearby->clear();
    for (const auto &v : peers) {
      const auto d = v.toObject();
      auto *row = new QListWidgetItem(QIcon::fromTheme("computer"),
                                      d["name"].toString(), nearby);
      row->setData(Qt::UserRole, d);
      row->setSizeHint(QSize(400, 48));
    }
    if (peers.isEmpty()) {
      auto *row = new QListWidgetItem(
          text("Noch kein Computer gefunden. AirLink auf dem anderen Gerät "
               "öffnen.",
               "No computer found yet. Open AirLink on the other device."),
          nearby);
      row->setFlags(Qt::NoItemFlags);
    }
    legacyRequests->clear();
    for (const auto &v : current["pairRequests"].toArray()) {
      const auto p = v.toObject();
      auto *row = new QListWidgetItem(legacyRequests);
      auto *w = new QWidget(legacyRequests);
      auto *l = new QHBoxLayout(w);
      l->addWidget(label(p["name"].toString(), w), 1);
      l->addWidget(button(text("Zulassen", "Allow"), w, [this, p] {
        action("pair.approve", {{"id", p["id"]}});
      }));
      l->addWidget(button(text("Ablehnen", "Deny"), w, [this, p] {
        action("pair.deny", {{"id", p["id"]}});
      }));
      row->setSizeHint(w->sizeHint());
      legacyRequests->setItemWidget(row, w);
    }
    legacyRequests->setVisible(legacyRequests->count() > 0);
  };
  connect(&client, &DaemonClient::statusChanged, dialog, update);
  const auto begin = [this, guard](QListWidgetItem *item) {
    if (!guard)
      return;
    const auto d = item->data(Qt::UserRole).toJsonObject();
    if (d.isEmpty())
      return;
    client.request("pairing.start",
                   {{"url", d["address"]}, {"fingerprint", d["fingerprint"]}},
                   [this, guard](QJsonValue v, QString e) {
                     if (!guard)
                       return;
                     if (!e.isEmpty())
                       issue(e);
                     else
                       guard->setProperty("session",
                                          v.toObject()["id"].toString());
                     client.refresh();
                   });
  };
  connect(nearby, &QListWidget::itemClicked, dialog, begin);
  connect(nearby, &QListWidget::itemActivated, dialog, begin);
  update();
  dialog->show();
}

void MainWindow::manageDevice() {
  const auto device = selectedDevice();
  if (device.isEmpty())
    return;
  auto *dialog = new QDialog(this);
  dialog->setAttribute(Qt::WA_DeleteOnClose);
  QPointer<QDialog> guard = dialog;
  dialog->setWindowTitle(device["name"].toString());
  auto *form = new QFormLayout(dialog);
  form->addRow(label(device["name"].toString(), dialog, true));
  QMap<QString, QCheckBox *> controls;
  const QMap<QString, QString> names{
      {"canSend", text("Darf Dateien an diesen Computer senden",
                       "May send files to this computer")},
      {"canReceive", text("Darf Dateien von diesem Computer empfangen",
                          "May receive files from this computer")},
      {"autoAccept", text("Eingehende Dateien automatisch annehmen",
                          "Automatically accept incoming files")},
      {"canClipboard",
       text("Zwischenablagetext austauschen", "Exchange clipboard text")},
      {"canRelay",
       text("Browser-Vermittlung erlauben", "Allow browser relay")}};
  for (auto it = names.begin(); it != names.end(); ++it) {
    auto *check = new QCheckBox(it.value(), dialog);
    check->setChecked(device[it.key()].toBool());
    controls[it.key()] = check;
    form->addRow(check);
  }
  form->addRow(button(
      text("Speichern", "Save"), dialog, [this, guard, device, controls] {
        auto latest = device;
        for (const auto &v : client.status()["devices"].toArray())
          if (v.toObject()["id"] == device["id"])
            latest = v.toObject();
        for (auto it = controls.begin(); it != controls.end(); ++it)
          if (it.value()->isChecked() != device[it.key()].toBool())
            latest[it.key()] = it.value()->isChecked();
        client.request("device.update", latest,
                       [this, guard](QJsonValue, QString e) {
                         if (!e.isEmpty())
                           issue(e);
                         else if (guard)
                           guard->close();
                         client.refresh();
                       });
      }));
  form->addRow(
      button(text("Vertrauen widerrufen …", "Revoke trust …"), dialog,
             [this, guard, device] {
               if (QMessageBox::question(
                       this, "AirLink",
                       text("Verbindung zu diesem Gerät aufheben? Laufende "
                            "Übertragungen werden abgebrochen.",
                            "Revoke this device? Active transfers will be "
                            "cancelled.")) == QMessageBox::Yes) {
                 action("device.revoke", {{"id", device["id"]}});
                 if (guard)
                   guard->close();
               }
             }));
  dialog->show();
}

void MainWindow::quit() {
  if (installingUpdate) {
    issue(text("Das geprüfte Update wird vorbereitet. Bitte kurz warten.",
               "The verified update is being prepared. Please wait."));
    return;
  }
  if (quitting)
    return;
  if (client.status()["activeTransferCount"].toInt() > 0 &&
      QMessageBox::question(
          this, "AirLink",
          text("Aktive Übertragungen sichern und AirLink beenden?",
               "Save active transfers and quit AirLink?")) != QMessageBox::Yes)
    return;
  client.shutdown([this](bool stopped) {
    if (stopped) {
      quitting = true;
      tray.hide();
      QApplication::quit();
    } else {
      activate();
      issue(text("Der Dienst konnte nicht sauber beendet werden. Bitte erneut "
                 "versuchen.",
                 "The service could not stop cleanly. Please retry."));
    }
  });
}

void MainWindow::shares() {
  auto *dialog = new QDialog(this);
  dialog->setAttribute(Qt::WA_DeleteOnClose);
  QPointer<QDialog> guard = dialog;
  dialog->setWindowTitle("Quick Share");
  dialog->resize(690, 420);
  auto *layout = new QVBoxLayout(dialog);
  layout->addWidget(label(
      text("Freigaben im lokalen Netzwerk", "Shares on your local network"),
      dialog, true));
  auto *list = new QListWidget(dialog);
  layout->addWidget(list, 1);
  const auto reload = [this, guard, list] {
    client.request("share.list", {},
                   [this, guard, list](QJsonValue value, QString e) {
                     if (!guard)
                       return;
                     if (!e.isEmpty()) {
                       issue(e);
                       return;
                     }
                     list->clear();
                     for (const auto &v : value.toArray()) {
                       const auto share = v.toObject();
                       auto *row = new QListWidgetItem(
                           share["name"].toString() + " · " +
                               bytes(share["totalBytes"].toInteger()) +
                               (share["revoked"].toBool()
                                    ? text(" · widerrufen", " · revoked")
                                    : QString{}),
                           list);
                       row->setData(Qt::UserRole, share);
                     }
                   });
  };
  auto *actions = new QHBoxLayout;
  actions->addWidget(button(
      text("Freigabe erstellen …", "Create share …"), dialog,
      [this, guard, reload] {
        if (!guard)
          return;
        const auto paths = QFileDialog::getOpenFileNames(
            guard, text("Dateien freigeben", "Share files"));
        if (paths.isEmpty() || !guard)
          return;
        auto *create = new QDialog(guard);
        create->setAttribute(Qt::WA_DeleteOnClose);
        QPointer<QDialog> createGuard = create;
        auto *form = new QFormLayout(create);
        auto *name = new QLineEdit(QFileInfo(paths.first()).fileName(), create);
        auto *password = new QLineEdit(create);
        password->setEchoMode(QLineEdit::Password);
        auto *limit = new QSpinBox(create);
        limit->setRange(0, 1000000);
        limit->setSpecialValueText(text("Unbegrenzt", "Unlimited"));
        auto *expiry = new QCheckBox(
            text("Ablaufdatum verwenden", "Set an expiry date"), create);
        auto *at =
            new QDateTimeEdit(QDateTime::currentDateTime().addDays(1), create);
        at->setCalendarPopup(true);
        at->setEnabled(false);
        connect(expiry, &QCheckBox::toggled, at, &QWidget::setEnabled);
        form->addRow(text("Name", "Name"), name);
        form->addRow(text("Passwort · optional", "Password · optional"),
                     password);
        form->addRow(text("Downloadlimit", "Download limit"), limit);
        form->addRow(expiry);
        form->addRow(at);
        auto *feedback = label("", create);
        form->addRow(feedback);
        form->addRow(
            button(text("Freigabe erstellen", "Create share"), create,
                   [this, createGuard, name, password, limit, expiry, at, paths,
                    feedback, reload] {
                     QJsonArray files;
                     for (const auto &p : paths)
                       files.append(p);
                     QJsonObject input{{"name", name->text()},
                                       {"paths", files},
                                       {"password", password->text()},
                                       {"maxDownloads", limit->value()}};
                     if (expiry->isChecked())
                       input["expiresAt"] =
                           at->dateTime().toUTC().toString(Qt::ISODate);
                     client.request("share.create", input,
                                    [this, createGuard, feedback,
                                     reload](QJsonValue, QString e) {
                                      if (!createGuard)
                                        return;
                                      if (!e.isEmpty()) {
                                        feedback->setText(e);
                                        return;
                                      }
                                      createGuard->close();
                                      reload();
                                      client.refresh();
                                    });
                   }));
        create->show();
      }));
  actions->addWidget(button(text("Link kopieren", "Copy link"), dialog, [list] {
    if (const auto *row = list->currentItem()) {
      const auto share = row->data(Qt::UserRole).toJsonObject();
      if (!share["revoked"].toBool())
        QApplication::clipboard()->setText(share["url"].toString());
    }
  }));
  actions->addWidget(
      button(text("Widerrufen", "Revoke"), dialog, [this, guard, list, reload] {
        const auto *row = list->currentItem();
        if (!row)
          return;
        client.request("share.revoke",
                       {{"id", row->data(Qt::UserRole).toJsonObject()["id"]}},
                       [this, guard, reload](QJsonValue, QString e) {
                         if (!guard)
                           return;
                         if (!e.isEmpty())
                           issue(e);
                         reload();
                       });
      }));
  layout->addLayout(actions);
  layout->addWidget(
      button(text("Fertig", "Done"), dialog, [dialog] { dialog->close(); }));
  reload();
  dialog->show();
}

void MainWindow::clipboard() {
  auto *dialog = new QDialog(this);
  dialog->setAttribute(Qt::WA_DeleteOnClose);
  QPointer<QDialog> guard = dialog;
  dialog->setWindowTitle(text("Zwischenablage", "Clipboard"));
  dialog->resize(650, 540);
  auto *layout = new QVBoxLayout(dialog);
  layout->addWidget(label(text("Text zwischen deinen Geräten teilen",
                               "Share text between your devices"),
                          dialog, true));
  auto *recipient = new QComboBox(dialog);
  for (const auto &v :
       sortedDevices(client.status()["devices"].toArray(), true)) {
    const auto d = v.toObject();
    if (d["canClipboard"].toBool())
      recipient->addItem(d["name"].toString(), d["id"]);
  }
  layout->addWidget(recipient);
  if (recipient->count() == 0)
    layout->addWidget(
        label(text("Aktiviere den Textaustausch zuerst in den Berechtigungen "
                   "beider Geräte.",
                   "Enable clipboard text in both devices' permissions first."),
              dialog));
  auto *draftText = new QTextEdit(dialog);
  draftText->setPlaceholderText(
      text("Text schreiben oder bewusst aus der Zwischenablage einfügen",
           "Type text or explicitly paste from the clipboard"));
  layout->addWidget(draftText);
  auto *sendActions = new QHBoxLayout;
  sendActions->addWidget(button(text("Einfügen", "Paste"), dialog, [draftText] {
    draftText->setPlainText(QApplication::clipboard()->text());
  }));
  auto *send = button(
      text("Text senden", "Send text"), dialog, [this, recipient, draftText] {
        action("clipboard.send",
               {{"deviceId", recipient->currentData().toString()},
                {"text", draftText->toPlainText()}});
      });
  send->setEnabled(recipient->count() > 0);
  sendActions->addWidget(send);
  layout->addLayout(sendActions);
  auto *received = new QListWidget(dialog);
  layout->addWidget(received, 1);
  const auto reload = [this, guard, received] {
    client.request("clipboard.list", {},
                   [this, guard, received](QJsonValue value, QString e) {
                     if (!guard)
                       return;
                     if (!e.isEmpty()) {
                       issue(e);
                       return;
                     }
                     received->clear();
                     for (const auto &v : value.toArray()) {
                       const auto message = v.toObject();
                       auto *row = new QListWidgetItem(
                           message["deviceName"].toString() + "\n" +
                               message["text"].toString().left(180),
                           received);
                       row->setData(Qt::UserRole, message);
                       row->setToolTip(message["text"].toString());
                     }
                   });
  };
  auto *receiveActions = new QHBoxLayout;
  receiveActions->addWidget(
      button(text("Ausgewählten Text kopieren", "Copy selected text"), dialog,
             [received] {
               if (const auto *row = received->currentItem())
                 QApplication::clipboard()->setText(
                     row->data(Qt::UserRole).toJsonObject()["text"].toString());
             }));
  receiveActions->addWidget(button(
      text("Entfernen", "Dismiss"), dialog, [this, guard, received, reload] {
        const auto *row = received->currentItem();
        if (!row)
          return;
        client.request("clipboard.dismiss",
                       {{"id", row->data(Qt::UserRole).toJsonObject()["id"]}},
                       [guard, reload](QJsonValue, QString) {
                         if (guard)
                           reload();
                       });
      }));
  receiveActions->addWidget(
      button(text("Aktualisieren", "Refresh"), dialog, reload));
  layout->addLayout(receiveActions);
  layout->addWidget(
      button(text("Fertig", "Done"), dialog, [dialog] { dialog->close(); }));
  reload();
  dialog->show();
}

void MainWindow::browserConnection() {
  auto *dialog = new QDialog(this);
  dialog->setAttribute(Qt::WA_DeleteOnClose);
  QPointer<QDialog> guard = dialog;
  dialog->setWindowTitle(text("Browser verbinden", "Connect a browser"));
  auto *layout = new QVBoxLayout(dialog);
  layout->addWidget(
      label(text("Handy oder Tablet verbinden", "Connect a phone or tablet"),
            dialog, true));
  layout->addWidget(
      label(text("Öffne diese Adresse im selben Netzwerk. Die Anleitung führt "
                 "durch Zertifikatsvertrauen und Verbindung.",
                 "Open this address on the same network. The guide explains "
                 "certificate trust and connection."),
            dialog));
  const auto urls = client.status()["setupURLs"].toArray();
  const auto address = urls.isEmpty() ? QString{} : urls.first().toString();
  auto *url = label(address, dialog);
  url->setTextInteractionFlags(Qt::TextSelectableByMouse);
  layout->addWidget(url);
  layout->addWidget(
      button(text("Adresse kopieren", "Copy address"), dialog,
             [address] { QApplication::clipboard()->setText(address); }));
  auto *qr = new QLabel(dialog);
  qr->setAlignment(Qt::AlignCenter);
  layout->addWidget(qr);
  layout->addWidget(label(text("Zertifikatsidentität dieses Computers:",
                               "Certificate identity of this computer:"),
                          dialog));
  auto *fingerprint = label(client.status()["fingerprint"].toString(), dialog);
  fingerprint->setTextInteractionFlags(Qt::TextSelectableByMouse);
  layout->addWidget(fingerprint);
  layout->addWidget(
      button(text("Fertig", "Done"), dialog, [dialog] { dialog->close(); }));
  client.request("connection.qr", {{"url", address}},
                 [guard, qr](QJsonValue value, QString e) {
                   if (!guard || !e.isEmpty())
                     return;
                   QPixmap image;
                   if (image.loadFromData(QByteArray::fromBase64(
                           value.toObject()["png"].toString().toLatin1())))
                     qr->setPixmap(image.scaled(256, 256, Qt::KeepAspectRatio,
                                                Qt::SmoothTransformation));
                 });
  dialog->resize(520, 640);
  dialog->show();
}

void MainWindow::fetchUpdate(const QUrl &url, qint64 limit,
                             std::function<void(QByteArray, QString)> done) {
  QNetworkRequest request(url);
  request.setTransferTimeout(20000);
  request.setAttribute(QNetworkRequest::RedirectPolicyAttribute,
                       QNetworkRequest::ManualRedirectPolicy);
  request.setAttribute(QNetworkRequest::CookieLoadControlAttribute,
                       QNetworkRequest::Manual);
  request.setAttribute(QNetworkRequest::CookieSaveControlAttribute,
                       QNetworkRequest::Manual);
  auto *reply = network.get(request);
  reply->setReadBufferSize(65536);
  auto content = std::make_shared<QByteArray>();
  connect(reply, &QIODevice::readyRead, this, [reply, content, limit] {
    *content += reply->readAll();
    if (content->size() > limit) {
      reply->setProperty("oversized", true);
      reply->abort();
    }
  });
  connect(reply, &QNetworkReply::finished, this,
          [reply, content, limit, done = std::move(done)] {
            *content += reply->readAll();
            const bool ok =
                reply->error() == QNetworkReply::NoError &&
                reply->attribute(QNetworkRequest::HttpStatusCodeAttribute)
                        .toInt() == 200 &&
                content->size() <= limit &&
                !reply->property("oversized").toBool();
            done(ok ? *content : QByteArray{},
                 ok ? QString{}
                    : QStringLiteral("The update source is unavailable. Check "
                                     "your internet connection or try later."));
            reply->deleteLater();
          });
}
void MainWindow::checkUpdates(bool automatic) {
  if (checkingUpdates || installingUpdate)
    return;
  checkingUpdates = true;
  if (!automatic) {
    activate();
    pages->setCurrentIndex(2);
    settingPages->setCurrentIndex(3);
  }
  updateFeedback->setText(
      text("Suche nach einer neuen Beta …", "Checking for a new beta …"));
  const QUrl feedUrl("https://raw.githubusercontent.com/lucaskleinsteuber/"
                     "Airlink/main/updates/linux-x64.json");
  fetchUpdate(
      feedUrl, 1024 * 1024,
      [this, automatic, feedUrl](QByteArray feed, QString error) {
        if (!error.isEmpty()) {
          checkingUpdates = false;
          updateFeedback->setText(error);
          return;
        }
        fetchUpdate(
            QUrl(feedUrl.toString() + ".signature"), 4096,
            [this, automatic, feed](QByteArray signedText, QString error) {
              checkingUpdates = false;
              if (!error.isEmpty()) {
                updateFeedback->setText(error);
                return;
              }
              const auto signature = QByteArray::fromBase64(
                  signedText.trimmed(),
                  QByteArray::AbortOnBase64DecodingErrors);
              QFile config(":/airlink/update-keys.json");
              if (!config.open(QIODevice::ReadOnly)) {
                updateFeedback->setText(
                    text("Updatekonfiguration fehlt.",
                         "Update configuration is missing."));
                return;
              }
              const auto root =
                  QJsonDocument::fromJson(config.readAll()).object();
              const auto pub = QByteArray::fromBase64(
                  root["publicKey"].toString().toLatin1(),
                  QByteArray::AbortOnBase64DecodingErrors);
              UpdateInfo info;
              if (!parseUpdate(feed, signature, pub,
                               QString::fromLatin1(Version), info, error)) {
                updateFeedback->setText(error);
                return;
              }
              updateFeedback->setText(
                  info.available
                      ? text("Neue Beta verfügbar: ", "New beta available: ") +
                            info.version
                      : text("Du nutzt die aktuelle Beta.",
                             "You are using the current beta."));
              if (!info.available)
                return;
              if (automatic && !client.status()["settings"]
                                    .toObject()["automaticUpdateChecks"]
                                    .toBool())
                return;
              if (automatic) {
                tray.showMessage("AirLink", text("Neue Beta verfügbar: ",
                                                 "New beta available: ") +
                                                info.version);
                return;
              }
              auto *dialog = new QDialog(this);
              dialog->setAttribute(Qt::WA_DeleteOnClose);
              auto *layout = new QVBoxLayout(dialog);
              layout->addWidget(label("AirLink " + info.version, dialog, true));
              if (!qEnvironmentVariable("APPIMAGE").isEmpty()) {
                layout->addWidget(
                    label(text("Das Update wird geprüft und anschließend mit "
                               "einem Neustart installiert.",
                               "The update will be verified and installed with "
                               "a restart."),
                          dialog));
                layout->addWidget(button(
                    text("Laden und installieren", "Download and install"),
                    dialog, [this, dialog, info, feed, signature, pub] {
                      dialog->close();
                      downloadUpdate(info.packages["appimage"], feed, signature,
                                     pub);
                    }));
              } else {
                layout->addWidget(label(
                    text("Diese Installation wird vom Paketmanager verwaltet. "
                         "Lade das passende geprüfte Paket und installiere es "
                         "mit deinem Paketmanager.",
                         "This installation is managed by your package "
                         "manager. Download the matching verified package and "
                         "install it with your package manager."),
                    dialog));
                QFile release("/etc/os-release");
                const auto native = release.open(QIODevice::ReadOnly)
                                        ? nativePackageFormat(QString::fromUtf8(
                                              release.read(16384)))
                                        : QString{};
                const auto formats = native.isEmpty()
                                         ? QStringList{"arch", "deb", "rpm"}
                                         : QStringList{native};
                for (const auto &format : formats)
                  layout->addWidget(button(
                      format == "arch"  ? "Arch · .pkg.tar.zst"
                      : format == "deb" ? "Debian / Ubuntu · .deb"
                                        : "Fedora / openSUSE · .rpm",
                      dialog,
                      [this, dialog, info, format, feed, signature, pub] {
                        dialog->close();
                        downloadUpdate(info.packages[format], feed, signature,
                                       pub);
                      }));
              }
              layout->addWidget(button(text("Später", "Later"), dialog,
                                       [dialog] { dialog->close(); }));
              dialog->show();
            });
      });
}
void MainWindow::maybeCheckUpdates() {
  const auto settings = client.status()["settings"].toObject();
  if (!settings["setupCompleted"].toBool() ||
      !settings["automaticUpdateChecks"].toBool())
    return;
  QSettings preferences;
  const auto now = QDateTime::currentSecsSinceEpoch();
  const auto last = preferences.value("updates/lastCheck", 0).toLongLong();
  if (last > 0 && now - last < 86400)
    return;
  preferences.setValue("updates/lastCheck", now);
  checkUpdates(true);
}
void MainWindow::downloadUpdate(const UpdateArtifact &artifact,
                                const QByteArray &feed,
                                const QByteArray &signature,
                                const QByteArray &pub) {
  const auto cache =
      QStandardPaths::writableLocation(QStandardPaths::GenericCacheLocation) +
      "/AirLink/updates";
  if (!QDir().mkpath(cache)) {
    issue(text("Updateordner ist nicht beschreibbar.",
               "The update cache is not writable."));
    return;
  }
  auto temporary = std::make_shared<QTemporaryDir>(cache + "/download-XXXXXX");
  if (!temporary->isValid()) {
    issue(text("Update konnte nicht vorbereitet werden.",
               "Could not prepare the update."));
    return;
  }
  const auto path = temporary->filePath(artifact.file);
  auto file = std::make_shared<QFile>(path);
  if (!file->open(QIODevice::WriteOnly | QIODevice::NewOnly)) {
    issue(text("Update konnte nicht gespeichert werden.",
               "Could not save the update."));
    return;
  }
  QNetworkRequest request(artifact.url);
  request.setTransferTimeout(30000);
  request.setAttribute(QNetworkRequest::RedirectPolicyAttribute,
                       QNetworkRequest::UserVerifiedRedirectPolicy);
  request.setMaximumRedirectsAllowed(5);
  request.setAttribute(QNetworkRequest::CookieLoadControlAttribute,
                       QNetworkRequest::Manual);
  request.setAttribute(QNetworkRequest::CookieSaveControlAttribute,
                       QNetworkRequest::Manual);
  auto *reply = network.get(request);
  reply->setReadBufferSize(1024 * 1024);
  updateFeedback->setText(
      text("Update wird geladen …", "Downloading update …"));
  connect(reply, &QNetworkReply::redirected, this, [reply](const QUrl &url) {
    const auto host = url.host();
    if (url.scheme() == "https" && url.userInfo().isEmpty() &&
        (host == "github.com" ||
         host == "release-assets.githubusercontent.com" ||
         host == "objects.githubusercontent.com"))
      reply->redirectAllowed();
    else
      reply->abort();
  });
  const auto read = [reply, file, artifact] {
    while (reply->bytesAvailable() > 0) {
      const auto block = reply->read(1024 * 1024);
      if (block.isEmpty())
        break;
      if (file->size() + block.size() > artifact.length ||
          file->write(block) != block.size()) {
        reply->setProperty("writeFailed", true);
        reply->abort();
        break;
      }
    }
  };
  connect(reply, &QIODevice::readyRead, this, read);
  connect(
      reply, &QNetworkReply::finished, this,
      [this, reply, file, temporary, path, artifact, feed, signature, pub,
       read] {
        read();
        file->close();
        const bool networkOK =
            reply->error() == QNetworkReply::NoError &&
            reply->attribute(QNetworkRequest::HttpStatusCodeAttribute)
                    .toInt() == 200 &&
            !reply->property("writeFailed").toBool();
        reply->deleteLater();
        QString error;
        if (!networkOK || !verifyUpdateFile(path, artifact, pub, error)) {
          updateFeedback->setText(
              error.isEmpty()
                  ? text("Download unterbrochen. Bitte erneut versuchen.",
                         "Download interrupted. Please retry.")
                  : error);
          return;
        }
        if (artifact.type != "appimage") {
          temporary->setAutoRemove(false);
          updateFeedback->setText(text("Geprüftes Paket gespeichert: ",
                                       "Verified package saved: ") +
                                  path);
          auto *dialog = new QDialog(this);
          dialog->setAttribute(Qt::WA_DeleteOnClose);
          auto *layout = new QVBoxLayout(dialog);
          auto *explanation =
              label(text("Installiere das Paket mit deinem Paketmanager. Deine "
                         "AirLink-Daten bleiben erhalten.",
                         "Install the package with your package manager. Your "
                         "AirLink data is preserved."),
                    dialog);
          layout->addWidget(explanation);
          const auto command =
              artifact.type == "arch"  ? QStringLiteral("sudo pacman -U ")
              : artifact.type == "deb" ? QStringLiteral("sudo apt install ")
              : !QStandardPaths::findExecutable("dnf").isEmpty()
                  ? QStringLiteral("sudo dnf install ")
              : !QStandardPaths::findExecutable("zypper").isEmpty()
                  ? QStringLiteral("sudo zypper install ")
                  : QStringLiteral("sudo rpm -U ");
          QString quoted = path;
          quoted.replace("'", "'\\''");
          auto *location = new QLineEdit(command + "'" + quoted + "'", dialog);
          location->setReadOnly(true);
          layout->addWidget(location);
          layout->addWidget(button(
              text("Installationsbefehl kopieren", "Copy install command"),
              dialog, [location] {
                QApplication::clipboard()->setText(location->text());
              }));
          layout->addWidget(
              button(text("Ordner öffnen", "Open folder"), dialog, [path] {
                QDesktopServices::openUrl(
                    QUrl::fromLocalFile(QFileInfo(path).absolutePath()));
              }));
          layout->addWidget(button(text("Fertig", "Done"), dialog,
                                   [dialog] { dialog->close(); }));
          dialog->show();
          return;
        }
        const auto target = qEnvironmentVariable("APPIMAGE");
        if (target.isEmpty() || QFileInfo(target).isSymLink() ||
            !QFileInfo(target).isWritable()) {
          issue(text("Diese AppImage kann nicht selbst ersetzt werden. Richte "
                     "sie im Benutzerverzeichnis ein.",
                     "This AppImage cannot replace itself. Install it in your "
                     "user directory."));
          return;
        }
        if (client.status()["activeTransferCount"].toInt() > 0 &&
            QMessageBox::question(this, "AirLink",
                                  text("Aktive Übertragungen sichern und das "
                                       "geprüfte Update installieren?",
                                       "Save active transfers and install the "
                                       "verified update?")) != QMessageBox::Yes)
          return;
        const auto feedPath = temporary->filePath("feed.json"),
                   sigPath = temporary->filePath("feed.signature"),
                   waiter = temporary->filePath("airlink-update");
        for (const auto &entry : QList<QPair<QString, QByteArray>>{
                 {feedPath, feed}, {sigPath, signature.toBase64()}}) {
          QFile output(entry.first);
          if (!output.open(QIODevice::WriteOnly | QIODevice::NewOnly) ||
              output.write(entry.second) != entry.second.size()) {
            issue(text("Update konnte nicht vorbereitet werden.",
                       "Could not prepare the update."));
            return;
          }
        }
        if (!QFile::copy(helperPath, waiter) ||
            !QFile::setPermissions(waiter, QFile::ReadOwner |
                                               QFile::WriteOwner |
                                               QFile::ExeOwner)) {
          issue(text("Update-Helfer konnte nicht vorbereitet werden.",
                     "Could not prepare the update helper."));
          return;
        }
        client.shutdown([this, temporary, path, target, feedPath, sigPath,
                         waiter](bool stopped) {
          if (!stopped) {
            issue(text(
                "Update abgebrochen: Der Transferdienst läuft noch.",
                "Update cancelled: the transfer service is still running."));
            return;
          }
          installingUpdate = true;
          qint64 pid = 0;
          const auto readyPath = temporary->filePath("ready.json");
          const QStringList args{
              "--apply-linux-update",
              "--parent-pid",
              QString::number(QCoreApplication::applicationPid()),
              "--parent-executable",
              QCoreApplication::applicationFilePath(),
              "--target",
              target,
              "--package",
              path,
              "--feed",
              feedPath,
              "--feed-signature",
              sigPath,
              "--ready-file",
              readyPath};
          if (!QProcess::startDetached(waiter, args, temporary->path(), &pid)) {
            issue(text("Update konnte nicht gestartet werden.",
                       "Could not start the update."));
            installingUpdate = false;
            start();
            return;
          }
          temporary->setAutoRemove(false);
          auto *readiness = new QTimer(this);
          readiness->setInterval(100);
          auto attempts = std::make_shared<int>(0);
          connect(
              readiness, &QTimer::timeout, this,
              [this, readiness, attempts, readyPath, pid, temporary] {
                QFile file(readyPath);
                QJsonObject response;
                if (file.open(QIODevice::ReadOnly) && file.size() <= 4096)
                  response = QJsonDocument::fromJson(file.readAll()).object();
                if (response["pid"].toInteger() == pid &&
                    response["ready"].toBool()) {
                  readiness->stop();
                  readiness->deleteLater();
                  quitting = true;
                  tray.hide();
                  QApplication::quit();
                  return;
                }
                if (++*attempts >= 300 || (response["pid"].toInteger() == pid &&
                                           response.contains("ready"))) {
                  readiness->stop();
                  readiness->deleteLater();
                  installingUpdate = false;
                  temporary->setAutoRemove(true);
                  issue(text("Die Updatevorbereitung ist fehlgeschlagen. Die "
                             "bisherige App bleibt verfügbar.",
                             "Update preparation failed. The current app "
                             "remains available."));
                  start();
                }
              });
          readiness->start();
        });
      });
}

void MainWindow::installAppImage() {
  const auto source = qEnvironmentVariable("APPIMAGE");
  if (source.isEmpty()) {
    QMessageBox::information(
        this, "AirLink",
        text("Diese Installation wird bereits vom Paketmanager im "
             "Anwendungsmenü eingerichtet.",
             "Your package manager already integrates this installation into "
             "the application menu."));
    return;
  }
  if (QFileInfo(source).isSymLink() || !QFileInfo(source).isFile()) {
    issue(text("Die AppImage-Datei ist nicht verfügbar.",
               "The AppImage file is unavailable."));
    return;
  }
  const auto root = QStandardPaths::writableLocation(
                 QStandardPaths::GenericDataLocation),
             directory = root + "/AirLink/bin",
             target = directory + "/AirLink.AppImage";
  if (QFileInfo(directory).isSymLink() || !QDir().mkpath(directory)) {
    issue(text("Anwendungsordner ist nicht beschreibbar.",
               "The application directory is not writable."));
    return;
  }
  QFile::setPermissions(directory,
                        QFile::ReadOwner | QFile::WriteOwner | QFile::ExeOwner);
  if (QFileInfo(source).canonicalFilePath() !=
      QFileInfo(target).canonicalFilePath()) {
    if (QFileInfo(target).isSymLink()) {
      issue(text("Der Zielpfad ist ungültig.",
                 "The destination path is invalid."));
      return;
    }
    QFile input(source);
    QSaveFile output(target);
    if (!input.open(QIODevice::ReadOnly) ||
        !output.open(QIODevice::WriteOnly)) {
      issue(text("AppImage konnte nicht eingerichtet werden.",
                 "Could not install the AppImage."));
      return;
    }
    while (!input.atEnd()) {
      const auto block = input.read(1024 * 1024);
      if (block.isEmpty() && input.error() != QFile::NoError) {
        output.cancelWriting();
        return;
      }
      if (output.write(block) != block.size()) {
        output.cancelWriting();
        return;
      }
    }
    output.setPermissions(QFile::ReadOwner | QFile::WriteOwner |
                          QFile::ExeOwner);
    if (!output.commit()) {
      issue(text("AppImage konnte nicht eingerichtet werden.",
                 "Could not install the AppImage."));
      return;
    }
  }
  const auto applications = root + "/applications",
             icons = root + "/icons/hicolor/scalable/apps";
  if (!QDir().mkpath(applications) || !QDir().mkpath(icons)) {
    issue(text("Anwendungsstarter konnte nicht gespeichert werden.",
               "Could not save the application launcher."));
    return;
  }
  QFile icon(":/airlink/icon.svg");
  if (!icon.open(QIODevice::ReadOnly))
    return;
  QSaveFile iconOutput(icons + "/io.github.lucaskleinsteuber.AirLink.svg");
  if (iconOutput.open(QIODevice::WriteOnly)) {
    iconOutput.write(icon.readAll());
    iconOutput.commit();
  }
  QSaveFile desktop(applications +
                    "/io.github.lucaskleinsteuber.AirLink.desktop");
  if (!desktop.open(QIODevice::WriteOnly)) {
    issue(text("Anwendungsstarter konnte nicht gespeichert werden.",
               "Could not save the application launcher."));
    return;
  }
  const auto entry =
      QStringLiteral("[Desktop Entry]\nType=Application\nName=AirLink\nExec=") +
      desktopQuoted(target) +
      " %F\nIcon=io.github.lucaskleinsteuber.AirLink\nTerminal="
      "false\nCategories=Network;FileTransfer;\nStartupWMClass=AirLink\n";
  desktop.write(entry.toUtf8());
  if (!desktop.commit()) {
    issue(text("Anwendungsstarter konnte nicht gespeichert werden.",
               "Could not save the application launcher."));
    return;
  }
  QMessageBox::information(
      this, "AirLink",
      text(
          "AirLink ist im Anwendungsmenü eingerichtet. Nach dem Beenden kannst "
          "du es dort starten und die heruntergeladene Datei entfernen.",
          "AirLink is in the application menu. After quitting, launch it "
          "there; you can remove the downloaded copy."));
}
} // namespace AirLink
