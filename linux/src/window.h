#pragma once
#include "core.h"
#include "daemon.h"
#include "updates.h"
#include <QHash>
#include <QMainWindow>
#include <QNetworkAccessManager>
#include <QPointer>
#include <QSet>
#include <QSystemTrayIcon>
#include <QTimer>

class QCheckBox;
class QDialog;
class QLabel;
class QLineEdit;
class QPushButton;
class QStackedWidget;
class QToolBar;
class QVBoxLayout;

namespace AirLink {
class DeviceTile;
class FlowLayout;
class PairingDialog;
class AddDeviceDialog;
class TransferRow;

class MainWindow final : public QMainWindow {
  Q_OBJECT
public:
  explicit MainWindow(bool demo = false, QWidget *parent = nullptr);
  void start();
  void acceptPaths(const QStringList &paths);
  void activate();
  // Renders the main states with sample data into PNG files (demo only).
  bool snapshot(const QString &directory);

private slots:
  void notificationAction(uint id, const QString &action);

protected:
  void closeEvent(QCloseEvent *event) override;
  void showEvent(QShowEvent *event) override;
  void hideEvent(QHideEvent *event) override;

private:
  DaemonClient client;
  QSystemTrayIcon tray;
  QNetworkAccessManager network;
  QString helperPath;
  bool demoMode = false, initialized = false, quitting = false,
       builtEnglish = false;
  QToolBar *toolbar = nullptr;
  QJsonObject lastStatus;
  QStringList pendingPaths;
  QString startupError;
  bool checkingUpdates = false, installingUpdate = false;
  QStackedWidget *pages = nullptr;
  // Startup / failure page
  QLabel *startupTitle = nullptr, *startupText = nullptr;
  QPushButton *startupRetry = nullptr, *startupFolder = nullptr;
  // Welcome page
  QLineEdit *welcomeName = nullptr;
  QLabel *welcomeFolder = nullptr;
  QString welcomePath;
  QCheckBox *welcomeAutostart = nullptr, *welcomeNotify = nullptr;
  bool welcomeFilled = false;
  // Home page
  QVBoxLayout *bannerBox = nullptr, *recentList = nullptr;
  QLabel *homeTitle = nullptr, *homeSubtitle = nullptr, *ipLink = nullptr;
  QWidget *tileHost = nullptr, *recentArea = nullptr;
  FlowLayout *tiles = nullptr;
  QStringList tileKeys, bannerKeys;
  QHash<QString, DeviceTile *> tileMap;
  QHash<QString, TransferRow *> rowMap;
  QStringList rowOrder;
  // State
  QSet<QString> preparing, shownPairings, notified;
  QHash<QString, QString> clipboardTexts;
  QHash<uint, QString> notificationTransfers;
  QPointer<PairingDialog> pairingDialog;
  QPointer<AddDeviceDialog> addDialog;
  QPointer<QDialog> settingsDialog, historyDialog;
  QPointer<QLabel> updateLabel;
  QString updateMessage;
  QTimer listenTimer;
  bool pairingOpen = false;

  bool english() const;
  QString text(const char *de, const char *en) const;
  QJsonObject settings() const;
  QJsonArray pairedDevices() const;
  QJsonArray nearbyDevices() const;
  QJsonObject device(const QString &id) const;
  QJsonObject pairing(const QString &id) const;
  QStringList localAddresses() const;

  void build();
  QWidget *buildStartup();
  QWidget *buildWelcome();
  QWidget *buildHome();
  void rebuildTrayMenu();
  void render();
  void renderBanners();
  void renderTiles();
  void renderRecent();
  void observe(const QJsonObject &previous, const QJsonObject &next);
  void updatePairingWindow();

  void issue(const QString &error);
  void action(const QString &method, const QJsonObject &params);
  void transferAction(const QString &kind, const QString &id);
  void send(const QStringList &paths, const QJsonObject &device);
  void waitForPreparation(const QString &id, const QString &deviceId,
                          const QString &name);
  void chooseFiles(const QJsonObject &device, bool folders = false);
  void chooseFilesForAnyDevice();
  void sendChooser(const QStringList &paths);
  void tileMenu(const QJsonObject &device, const QPoint &global);
  void connectDevice(const QJsonObject &device);
  void connectAddress(const QString &address,
                      std::function<void(QString)> failed);
  void showPairing(const QString &id);
  void showAddDevice(bool byAddress = false);
  void deviceSettings(const QString &id);
  void updateDevice(QJsonObject device);
  void removeDevice(const QJsonObject &device);
  void sendClipboard(const QJsonObject &device);
  void reveal(const QString &id);
  void openReceiveFolder();
  void chooseReceiveFolder();
  void finishWelcome();
  void openSettings(int tab = 0);
  void updateSettings(std::function<void(QJsonObject &)> change);
  void openHistory();
  void showHelp();
  bool autostartEnabled() const;
  void setAutostart(bool enabled);
  void notify(const QString &key, const QString &title, const QString &body,
              const QString &transferId = {});
  void setUpdateMessage(const QString &message);
  void quit();

  void checkUpdates(bool automatic = false);
  void installAppImage();
  void exportDiagnostics();
  void maybeCheckUpdates();
  void fetchUpdate(const QUrl &url, qint64 limit,
                   std::function<void(QByteArray, QString)> done);
  void downloadUpdate(const UpdateArtifact &artifact, const QByteArray &feed,
                      const QByteArray &signature, const QByteArray &publicKey);
};
} // namespace AirLink
