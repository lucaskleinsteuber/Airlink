#include "updates.h"
#include <QCoreApplication>
#include <QFile>
#include <QJsonDocument>
#include <QJsonObject>
#include <iostream>
static QByteArray read(const QString &path, qint64 maximum) {
  QFile f(path);
  if (!f.open(QIODevice::ReadOnly) || f.size() > maximum)
    return {};
  return f.readAll();
}
int main(int argc, char **argv) {
  QCoreApplication app(argc, argv);
  const auto args = app.arguments();
  if (args.size() != 5) {
    std::cerr
        << "Expected feed, signature, public configuration and package.\n";
    return 2;
  }
  const auto config = QJsonDocument::fromJson(read(args[3], 16384)).object();
  const auto publicKey =
      QByteArray::fromBase64(config["publicKey"].toString().toLatin1(),
                             QByteArray::AbortOnBase64DecodingErrors);
  AirLink::UpdateInfo info;
  QString error;
  if (!AirLink::parseUpdate(
          read(args[1], 1048576),
          QByteArray::fromBase64(read(args[2], 4096).trimmed(),
                                 QByteArray::AbortOnBase64DecodingErrors),
          publicKey, "0.11.0", info, error)) {
    std::cerr << error.toStdString() << '\n';
    return 1;
  }
  bool found = false;
  for (const auto &a : info.packages)
    if (args[4].endsWith(a.file)) {
      found = true;
      if (!AirLink::verifyUpdateFile(args[4], a, publicKey, error)) {
        std::cerr << error.toStdString() << '\n';
        return 1;
      }
    }
  if (!found) {
    std::cerr << "Package does not belong to the feed.\n";
    return 1;
  }
  std::cout << "Verified Linux " << info.version.toStdString()
            << " package and feed.\n";
  return 0;
}
