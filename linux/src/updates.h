#pragma once
#include <QByteArray>
#include <QMap>
#include <QString>
#include <QUrl>

namespace AirLink {
struct UpdateArtifact {
  QString type, file, sha256;
  QUrl url;
  qint64 length = 0;
  QByteArray signature;
};
struct UpdateInfo {
  QString version;
  QMap<QString, UpdateArtifact> packages;
  bool available = false;
};
int compareVersions(const QString &a, const QString &b, bool *valid = nullptr);
bool verifyEd25519(const QByteArray &message, const QByteArray &signature,
                   const QByteArray &publicKey);
bool parseUpdate(const QByteArray &feed, const QByteArray &signature,
                 const QByteArray &publicKey, const QString &installed,
                 UpdateInfo &info, QString &error);
bool verifyUpdateFile(const QString &path, const UpdateArtifact &artifact,
                      const QByteArray &publicKey, QString &error);
} // namespace AirLink
