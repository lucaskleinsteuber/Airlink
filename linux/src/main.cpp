#include "version.h"
#include "window.h"
#include <QApplication>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <QLocalServer>
#include <QLocalSocket>
#include <QLockFile>
#include <QStandardPaths>
#include <iostream>
#include <memory>

int main(int argc, char **argv) {
  // Artifact/runtime checks exit before constructing a GUI application.
  for (int i = 1; i < argc; ++i)
    if (QByteArray(argv[i]) == "--version") {
      std::cout << AirLink::Version << '\n';
      return 0;
    }
  QApplication app(argc, argv);
  app.setApplicationName("AirLink");
  app.setOrganizationName("AirLink");
  app.setApplicationVersion(AirLink::Version);
  app.setDesktopFileName("io.github.lucaskleinsteuber.AirLink");
  app.setQuitOnLastWindowClosed(false);
  QStringList paths;
  for (int i = 1; i < argc; ++i) {
    const QString p = QString::fromLocal8Bit(argv[i]);
    if (QDir::isAbsolutePath(p))
      paths.append(p);
  }
  const QString runtime =
      QStandardPaths::writableLocation(QStandardPaths::RuntimeLocation);
  if (runtime.isEmpty()) {
    std::cerr << "A private desktop runtime directory is required.\n";
    return 1;
  }
  const QString directory = QDir(runtime).filePath("airlink");
  if (QFileInfo(directory).isSymLink() || !QDir().mkpath(directory) ||
      !QFile::setPermissions(directory, QFile::ReadOwner | QFile::WriteOwner |
                                            QFile::ExeOwner))
    return 1;
  const QString socket = directory + "/desktop.sock";
  QLockFile lock(directory + "/desktop.lock");
  lock.setStaleLockTime(0);
  if (!lock.tryLock(0)) {
    QLocalSocket existing;
    existing.connectToServer(socket);
    if (!existing.waitForConnected(2000)) {
      std::cerr << "AirLink is starting or still running. Try again shortly.\n";
      return 1;
    }
    QJsonArray files;
    for (const auto &p : paths)
      files.append(p);
    existing.write(QJsonDocument(files).toJson(QJsonDocument::Compact) + '\n');
    existing.waitForBytesWritten(1000);
    return 0;
  }
  QLocalServer server;
  server.setSocketOptions(QLocalServer::UserAccessOption);
  QLocalServer::removeServer(socket);
  if (!server.listen(socket)) {
    std::cerr << "Could not create the private AirLink activation socket.\n";
    return 1;
  }
  AirLink::MainWindow window;
  QObject::connect(&server, &QLocalServer::newConnection, &window, [&] {
    while (auto *peer = server.nextPendingConnection()) {
      auto buffer = std::make_shared<QByteArray>();
      QObject::connect(
          peer, &QLocalSocket::readyRead, &window, [peer, buffer, &window] {
            *buffer += peer->readAll();
            if (buffer->size() > 2 * 1024 * 1024) {
              peer->disconnectFromServer();
              return;
            }
            if (!buffer->contains('\n'))
              return;
            QStringList incoming;
            const auto array =
                QJsonDocument::fromJson(buffer->left(buffer->indexOf('\n')))
                    .array();
            for (const auto &v : array)
              if (QDir::isAbsolutePath(v.toString()))
                incoming.append(v.toString());
            window.activate();
            window.acceptPaths(incoming);
            peer->disconnectFromServer();
          });
      QObject::connect(peer, &QLocalSocket::disconnected, peer,
                       &QObject::deleteLater);
    }
  });
  window.show();
  window.acceptPaths(paths);
  window.start();
  return app.exec();
}
