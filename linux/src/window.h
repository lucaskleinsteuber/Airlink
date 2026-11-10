#pragma once
#include "core.h"
#include "daemon.h"
#include "updates.h"
#include <QMainWindow>
#include <QNetworkAccessManager>
#include <QPalette>
#include <QPointer>
#include <QSet>
#include <QSystemTrayIcon>

class QLabel;
class QListWidget;
class QStackedWidget;
class QTableWidget;
class QPushButton;
class QDialog;
class QTreeWidget;

namespace AirLink {
class MainWindow final : public QMainWindow {
  Q_OBJECT
public:
  explicit MainWindow(QWidget *parent = nullptr);
  void start();
  void acceptPaths(const QStringList &paths);
  void activate();

protected:
  void closeEvent(QCloseEvent *event) override;

private:
  DaemonClient client;
  QSystemTrayIcon tray;
  QNetworkAccessManager network;
  SettingsDraft draft;
  QPalette systemPalette;
  QString appliedAppearance;
  bool appliedEnglish = false;
  void applyPresentation();
  void rebuildTrayMenu();
  SourceSelection selection;
  QString selected, activePreparation, historyCursor, helperPath;
  QStringList submittedIdentities;
  QJsonObject retryBatch;
  QJsonArray historyRows;
  QSet<QString> seenCompletions, seenPairs;
  bool initialized = false, settingsLoaded = false, applyingEditors = false,
       preparing = false, saving = false, historyLoading = false,
       quitting = false;
  int settingsPane = 0;
  quint64 historyGeneration = 0;
  QStackedWidget *pages = nullptr, *settingPages = nullptr;
  QListWidget *devices = nullptr;
  QLabel *title = nullptr, *subtitle = nullptr, *message = nullptr,
         *prepared = nullptr, *attention = nullptr, *ownName = nullptr,
         *settingsFeedback = nullptr, *updateFeedback = nullptr;
  QTableWidget *currentTransfers = nullptr, *historyTransfers = nullptr;
  QPushButton *sendButton = nullptr, *chooseButton = nullptr,
              *cancelPreparation = nullptr, *retryButton = nullptr,
              *historyMore = nullptr, *saveButton = nullptr,
              *discardButton = nullptr;
  QMap<QString, QWidget *> editors;
  QPointer<QDialog> pairingDialog;
  void build();
  QWidget *buildSettings();
  void render();
  void renderTransfers(QTableWidget *table, const QJsonArray &rows);
  void applyEditors();
  void saveSettings();
  void setDraftValue(const QString &key, QJsonValue value);
  void choose(bool folders = false);
  void sendPrepared();
  void submit(QJsonObject batch, QStringList identities = {});
  QJsonObject selectedDevice() const;
  bool english() const;
  QString text(const char *de, const char *en) const;
  void action(const QString &method, const QJsonObject &params);
  void issue(const QString &error);
  void setup();
  void connectDevice(const QString &session = {});
  void manageDevice();
  void transferMenu(const QJsonObject &transfer);
  void transferDetails(const QJsonObject &transfer);
  void loadFilePage(QPointer<QDialog> dialog, QTreeWidget *files,
                    QPushButton *more, const QString &id, int offset);
  void reveal(const QString &id);
  void loadHistory(bool more = false);
  void shares();
  void clipboard();
  void browserConnection();
  void quit();
  void checkUpdates(bool automatic = false);
  void installAppImage();
  void maybeCheckUpdates();
  void fetchUpdate(const QUrl &url, qint64 limit,
                   std::function<void(QByteArray, QString)> done);
  void downloadUpdate(const UpdateArtifact &artifact, const QByteArray &feed,
                      const QByteArray &signature, const QByteArray &publicKey);
  bool checkingUpdates = false;
  bool installingUpdate = false;
};
} // namespace AirLink
