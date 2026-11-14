// Signed beta updates and the diagnostic report (moved unchanged from the
// previous window implementation; only the feedback target changed).
#include "version.h"
#include "widgets.h"
#include "window.h"
#include <QApplication>
#include <QDateTime>
#include <QDesktopServices>
#include <QDialog>
#include <QDir>
#include <QFile>
#include <QFileDialog>
#include <QFileInfo>
#include <QJsonDocument>
#include <QClipboard>
#include <QLabel>
#include <QLineEdit>
#include <QMessageBox>
#include <QNetworkReply>
#include <QProcess>
#include <QPushButton>
#include <QSaveFile>
#include <QSettings>
#include <QStandardPaths>
#include <QTemporaryDir>
#include <QTimer>
#include <QVBoxLayout>
#include <memory>

namespace AirLink {
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
    openSettings(2);
  }
  setUpdateMessage(
      text("Suche nach einer neuen Beta …", "Checking for a new beta …"));
  const QUrl feedUrl("https://raw.githubusercontent.com/lucaskleinsteuber/"
                     "Airlink/main/updates/linux-x64.json");
  fetchUpdate(
      feedUrl, 1024 * 1024,
      [this, automatic, feedUrl](QByteArray feed, QString error) {
        if (!error.isEmpty()) {
          checkingUpdates = false;
          setUpdateMessage(error);
          return;
        }
        fetchUpdate(
            QUrl(feedUrl.toString() + ".signature"), 4096,
            [this, automatic, feed](QByteArray signedText, QString error) {
              checkingUpdates = false;
              if (!error.isEmpty()) {
                setUpdateMessage(error);
                return;
              }
              const auto signature = QByteArray::fromBase64(
                  signedText.trimmed(),
                  QByteArray::AbortOnBase64DecodingErrors);
              QFile config(":/airlink/update-keys.json");
              if (!config.open(QIODevice::ReadOnly)) {
                setUpdateMessage(
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
                setUpdateMessage(error);
                return;
              }
              setUpdateMessage(
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
  setUpdateMessage(
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
          setUpdateMessage(
              error.isEmpty()
                  ? text("Download unterbrochen. Bitte erneut versuchen.",
                         "Download interrupted. Please retry.")
                  : error);
          return;
        }
        if (artifact.type != "appimage") {
          temporary->setAutoRemove(false);
          setUpdateMessage(text("Geprüftes Paket gespeichert: ",
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

void MainWindow::exportDiagnostics() {
  QPointer<MainWindow> self(this);
  auto save = [self](QJsonValue value, QString error) {
    if (!self)
      return;
    if (!error.isEmpty()) {
      self->issue(error);
      return;
    }
    const auto path = QFileDialog::getSaveFileName(
        self, self->text("Diagnosebericht speichern", "Save diagnostic report"),
        "AirLink-Diagnose.json", "JSON (*.json)");
    if (path.isEmpty())
      return;
    QSaveFile output(path);
    const auto data =
        QJsonDocument(value.toObject()).toJson(QJsonDocument::Indented);
    if (!output.open(QIODevice::WriteOnly) ||
        output.write(data) != data.size() || !output.commit()) {
      self->issue(
          self->text("Der Diagnosebericht konnte nicht gespeichert werden.",
                     "Could not save the diagnostic report."));
    }
  };
  if (client.ready())
    client.request("diagnostics.export", {}, save);
  else
    save(QJsonObject{{"schemaVersion", 1},
                     {"version", QString::fromLatin1(Version)},
                     {"platform", "linux"},
                     {"service", "unavailable"},
                     {"privacy", "No raw logs or personal data included."}},
         {});
}
} // namespace AirLink
