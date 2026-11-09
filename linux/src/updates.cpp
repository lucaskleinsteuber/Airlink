#include "updates.h"
#include <QCryptographicHash>
#include <QFile>
#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QRegularExpression>
#include <array>
#include <limits>
#include <memory>
#include <openssl/evp.h>

namespace AirLink {
static bool versionParts(const QString &s, std::array<quint64, 4> &out) {
  static const QRegularExpression re("^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|["
                                     "1-9][0-9]*)(?:-beta\\.([1-9][0-9]*))?$");
  const auto m = re.match(s);
  if (!m.hasMatch())
    return false;
  for (int i = 0; i < 3; ++i) {
    bool ok = false;
    out[i] = m.captured(i + 1).toULongLong(&ok);
    if (!ok)
      return false;
  }
  out[3] = std::numeric_limits<quint64>::max();
  if (!m.captured(4).isEmpty()) {
    bool ok = false;
    out[3] = m.captured(4).toULongLong(&ok);
    if (!ok || out[3] == std::numeric_limits<quint64>::max())
      return false;
  }
  return true;
}
int compareVersions(const QString &a, const QString &b, bool *valid) {
  std::array<quint64, 4> x{}, y{};
  const bool ok = versionParts(a, x) && versionParts(b, y);
  if (valid)
    *valid = ok;
  if (!ok || x == y)
    return 0;
  return x < y ? -1 : 1;
}
static bool verifyRaw(const unsigned char *data, size_t length,
                      const QByteArray &sig, const QByteArray &pub) {
  if (sig.size() != 64 || pub.size() != 32)
    return false;
  std::unique_ptr<EVP_PKEY, decltype(&EVP_PKEY_free)> key(
      EVP_PKEY_new_raw_public_key(
          EVP_PKEY_ED25519, nullptr,
          reinterpret_cast<const unsigned char *>(pub.constData()), 32),
      EVP_PKEY_free);
  std::unique_ptr<EVP_MD_CTX, decltype(&EVP_MD_CTX_free)> context(
      EVP_MD_CTX_new(), EVP_MD_CTX_free);
  return key && context &&
         EVP_DigestVerifyInit(context.get(), nullptr, nullptr, nullptr,
                              key.get()) == 1 &&
         EVP_DigestVerify(
             context.get(),
             reinterpret_cast<const unsigned char *>(sig.constData()), 64, data,
             length) == 1;
}
bool verifyEd25519(const QByteArray &body, const QByteArray &sig,
                   const QByteArray &pub) {
  return verifyRaw(reinterpret_cast<const unsigned char *>(body.constData()),
                   static_cast<size_t>(body.size()), sig, pub);
}
static QByteArray strictBase64(const QString &text, int size) {
  const auto result = QByteArray::fromBase64Encoding(
      text.toLatin1(), QByteArray::AbortOnBase64DecodingErrors);
  return result && result.decoded.size() == size ? result.decoded
                                                 : QByteArray{};
}
bool parseUpdate(const QByteArray &feed, const QByteArray &sig,
                 const QByteArray &pub, const QString &installed,
                 UpdateInfo &info, QString &error) {
  info = {};
  error.clear();
  if (feed.isEmpty() || feed.size() > 1024 * 1024 ||
      !verifyEd25519(feed, sig, pub)) {
    error = "The update feed signature is invalid.";
    return false;
  }
  QJsonParseError e;
  const auto root = QJsonDocument::fromJson(feed, &e).object();
  if (e.error != QJsonParseError::NoError || root["schemaVersion"] != 1 ||
      root["channel"] != "beta" || root["platform"] != "linux" ||
      root["architecture"] != "x64") {
    error = "The update feed is not a supported Linux beta feed.";
    return false;
  }
  info.version = root["version"].toString();
  bool valid = false;
  info.available = compareVersions(info.version, installed, &valid) > 0;
  if (!valid) {
    error = "The update version is invalid.";
    return false;
  }
  const QMap<QString, QString> formats{{"appimage", ".AppImage"},
                                       {"deb", ".deb"},
                                       {"rpm", ".rpm"},
                                       {"arch", ".pkg.tar.zst"}};
  for (const auto &v : root["packages"].toArray()) {
    const auto p = v.toObject();
    UpdateArtifact a;
    a.type = p["type"].toString();
    a.file = p["file"].toString();
    a.url = QUrl(p["url"].toString());
    a.sha256 = p["sha256"].toString();
    a.length = p["length"].toInteger();
    a.signature = strictBase64(p["ed25519Signature"].toString(), 64);
    const auto expected = QStringLiteral("AirLink-") + info.version +
                          "-linux-x64" + formats.value(a.type);
    if (!formats.contains(a.type) || a.file != expected ||
        a.url.toString() !=
            QStringLiteral("https://github.com/lucaskleinsteuber/Airlink/"
                           "releases/download/v") +
                info.version + "/" + expected ||
        a.length <= 0 || a.length > 512 * 1024 * 1024 ||
        a.signature.size() != 64 ||
        !QRegularExpression("^[0-9a-f]{64}$").match(a.sha256).hasMatch() ||
        info.packages.contains(a.type)) {
      error = "The update package metadata is invalid.";
      return false;
    }
    info.packages.insert(a.type, a);
  }
  if (info.packages.size() != formats.size()) {
    error = "The update feed is incomplete.";
    return false;
  }
  return true;
}
bool verifyUpdateFile(const QString &path, const UpdateArtifact &a,
                      const QByteArray &pub, QString &error) {
  const QFileInfo stat(path);
  if (stat.isSymLink() || !stat.isFile() || stat.size() != a.length ||
      a.length <= 0 || a.length > 512 * 1024 * 1024) {
    error = "The downloaded update size is invalid.";
    return false;
  }
  QFile file(path);
  if (!file.open(QIODevice::ReadOnly)) {
    error = "The update could not be opened.";
    return false;
  }
  QCryptographicHash hash(QCryptographicHash::Sha256);
  if (!hash.addData(&file) ||
      QString::fromLatin1(hash.result().toHex()) != a.sha256) {
    error = "The downloaded update checksum is invalid.";
    return false;
  }
  auto *mapped = file.map(0, a.length);
  if (!mapped) {
    error = "The update could not be verified.";
    return false;
  }
  const bool ok =
      verifyRaw(mapped, static_cast<size_t>(a.length), a.signature, pub);
  file.unmap(mapped);
  if (!ok) {
    error = "The downloaded update signature is invalid.";
    return false;
  }
  error.clear();
  return true;
}
} // namespace AirLink
