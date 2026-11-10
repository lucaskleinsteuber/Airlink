#include "core.h"
#include <QCollator>
#include <QDir>
#include <QFileInfo>
#include <QLocale>
#include <QRegularExpression>
#include <QStandardPaths>
#include <QUuid>
#include <algorithm>
#include <cmath>

namespace AirLink {
void SourceSelection::append(const QStringList &paths) {
  for (const auto &p : paths)
    if (QDir::isAbsolutePath(p))
      entries.append(
          {QUuid::createUuid().toString(QUuid::Id128), QDir::cleanPath(p)});
}
QStringList SourceSelection::paths() const {
  QStringList result;
  for (const auto &e : entries)
    result.append(e.path);
  return result;
}
QStringList SourceSelection::identities() const {
  QStringList result;
  for (const auto &e : entries)
    result.append(e.id);
  return result;
}
void SourceSelection::acknowledge(const QStringList &ids) {
  entries.removeIf([&](const SelectedSource &e) { return ids.contains(e.id); });
}
static QJsonObject mergeObjects(const QJsonObject &base,
                                const QJsonObject &draft, QJsonObject latest) {
  for (auto it = draft.begin(); it != draft.end(); ++it) {
    if (it.value() == base.value(it.key()))
      continue;
    if (it.value().isObject() && base.value(it.key()).isObject())
      latest[it.key()] =
          mergeObjects(base.value(it.key()).toObject(), it.value().toObject(),
                       latest.value(it.key()).toObject());
    else
      latest[it.key()] = it.value();
  }
  return latest;
}
void SettingsDraft::reset(const QJsonObject &v) {
  baseline = v;
  values = v;
}
void SettingsDraft::rebase(const QJsonObject &v) {
  values = merged(v);
  baseline = v;
}
QJsonObject SettingsDraft::merged(const QJsonObject &latest) const {
  return mergeObjects(baseline, values, latest);
}
bool SettingsDraft::dirty() const { return baseline != values; }
QString SettingsDraft::validate() const {
  const auto name = values["name"].toString().trimmed();
  if (name.isEmpty() || name.toUtf8().size() > 120)
    return QStringLiteral("name");
  for (const auto c : name)
    if (c.category() == QChar::Other_Control)
      return QStringLiteral("name");
  const auto folder = values["receiveDirectory"].toString();
  if (!QDir::isAbsolutePath(folder) || folder.contains(QChar(0)))
    return QStringLiteral("receiveDirectory");
  for (const auto key :
       {"sendLimitBytesPerSecond", "receiveLimitBytesPerSecond"}) {
    if (values.contains(key) && !values[key].isDouble())
      return QString::fromLatin1(key);
    const double n = values[key].toDouble();
    if (!std::isfinite(n) || n < 0 || n > 1099511627776.0 ||
        (n > 0 && n < 1024) || std::floor(n) != n)
      return QString::fromLatin1(key);
  }
  const auto organization = values["organization"].toObject();
  if (organization.contains("rules") && !organization["rules"].isArray())
    return QStringLiteral("organization");
  QSet<QString> ids;
  for (const auto &value : organization["rules"].toArray()) {
    const auto r = value.toObject();
    const auto id = r["id"].toString(), folder = r["folder"].toString();
    if (id.isEmpty() || ids.contains(id) || folder.isEmpty() ||
        QDir::isAbsolutePath(folder) || QDir::cleanPath(folder) != folder ||
        folder == "." || folder.startsWith("../") ||
        folder.contains(QRegularExpression("[<>:\"\\\\|?*]")))
      return QStringLiteral("organization");
    ids.insert(id);
    const auto extensions = r["extensions"].toArray();
    if (extensions.isEmpty())
      return QStringLiteral("organization");
    for (const auto &ext : extensions)
      if (!QRegularExpression("^\\.?[a-zA-Z0-9_-]{1,32}$")
               .match(ext.toString())
               .hasMatch())
        return QStringLiteral("organization");
  }
  return {};
}
bool terminal(const QString &s) {
  return s == "completed" || s == "failed" || s == "cancelled";
}
QString bytes(qint64 n) {
  double v = static_cast<double>(std::max<qint64>(0, n));
  const QStringList units{"B", "KiB", "MiB", "GiB", "TiB"};
  int i = 0;
  while (v >= 1024 && i < units.size() - 1) {
    v /= 1024;
    ++i;
  }
  return QLocale().toString(v, 'f', i == 0 ? 0 : 1) + " " + units[i];
}
QString speed(double n) {
  return std::isfinite(n) && n > 0 ? bytes(static_cast<qint64>(n)) + "/s"
                                   : QStringLiteral("—");
}
QString remaining(const QJsonObject &t) {
  const double rate = t["bytesPerSecond"].toDouble();
  if (t["state"] != "running" || !std::isfinite(rate) || rate <= 0)
    return QStringLiteral("—");
  const double left = std::max(0.0, t["totalBytes"].toDouble() -
                                        t["completedBytes"].toDouble() -
                                        t["skippedBytes"].toDouble());
  const double seconds = std::ceil(left / rate);
  if (!std::isfinite(seconds) || seconds > 31536000)
    return QStringLiteral("—");
  const qint64 n = static_cast<qint64>(seconds);
  return n >= 3600 ? QString::number(n / 3600) + " h " +
                         QString::number(n % 3600 / 60) + " min"
         : n >= 60 ? QString::number(n / 60) + " min " +
                         QString::number(n % 60) + " s"
                   : QString::number(n) + " s";
}
QString transferTitle(const QJsonObject &t) {
  if (!t["title"].toString().isEmpty())
    return t["title"].toString();
  const auto files = t["files"].toArray();
  return files.isEmpty() ? QStringLiteral("Transfer")
                         : files.first().toObject()["path"].toString();
}
QString stateLabel(const QString &s, bool en) {
  const QMap<QString, QPair<QString, QString>> names{
      {"offered", {"Wartet auf Annahme", "Waiting for acceptance"}},
      {"queued", {"Wartet", "Queued"}},
      {"running", {"Wird übertragen", "Transferring"}},
      {"paused", {"Pausiert", "Paused"}},
      {"reconnecting", {"Verbindung unterbrochen", "Connection interrupted"}},
      {"verifying", {"Wird geprüft", "Verifying"}},
      {"checkingDuplicate", {"Prüft auf Duplikate", "Checking duplicates"}},
      {"waitingDuplicate", {"Entscheidung erforderlich", "Decision needed"}},
      {"completed", {"Fertig", "Completed"}},
      {"failed", {"Fehlgeschlagen", "Failed"}},
      {"cancelled", {"Abgebrochen", "Cancelled"}},
      {"preparing", {"Wird vorbereitet", "Preparing"}}};
  const auto it = names.constFind(s);
  return it == names.end() ? s : (en ? it->second : it->first);
}
QJsonArray visibleTransfers(const QJsonArray &input, const QString &device,
                            int recent) {
  QList<QJsonObject> active, finished;
  for (const auto &v : input) {
    const auto t = v.toObject();
    if (!device.isEmpty() && t["deviceId"].toString() != device)
      continue;
    (terminal(t["state"].toString()) ? finished : active).append(t);
  }
  const auto newer = [](const QJsonObject &a, const QJsonObject &b) {
    const auto x = a["updatedAt"].toString(), y = b["updatedAt"].toString();
    return x == y ? a["id"].toString() > b["id"].toString() : x > y;
  };
  std::sort(active.begin(), active.end(), newer);
  std::sort(finished.begin(), finished.end(), newer);
  QJsonArray out;
  for (const auto &v : active)
    out.append(v);
  for (int i = 0; i < std::min(recent, static_cast<int>(finished.size())); ++i)
    out.append(finished[i]);
  return out;
}
static QString timestampKey(const QString &raw) {
  if (raw.endsWith('Z') && raw.size() >= 20 &&
      (raw[19] == '.' || raw[19] == 'Z')) {
    const auto fraction =
        raw[19] == '.' ? raw.mid(20, raw.size() - 21) : QString{};
    return raw.left(19) + fraction.leftJustified(9, '0', true);
  }
  return raw;
}
QJsonArray mergeHistory(const QJsonArray &history, const QJsonArray &summary) {
  QMap<QString, QJsonObject> values;
  for (const auto &v : history)
    values[v.toObject()["id"].toString()] = v.toObject();
  for (const auto &v : summary) {
    const auto t = v.toObject();
    const auto id = t["id"].toString();
    if ((!terminal(t["state"].toString()) || values.contains(id)) &&
        (!values.contains(id) ||
         timestampKey(t["updatedAt"].toString()) >=
             timestampKey(values[id]["updatedAt"].toString())))
      values[id] = t;
  }
  QList<QJsonObject> ordered = values.values();
  std::sort(ordered.begin(), ordered.end(),
            [](const QJsonObject &a, const QJsonObject &b) {
              const bool at = terminal(a["state"].toString()),
                         bt = terminal(b["state"].toString());
              if (at != bt)
                return !at;
              if (a["createdAt"] != b["createdAt"])
                return a["createdAt"].toString() > b["createdAt"].toString();
              return a["id"].toString() > b["id"].toString();
            });
  QJsonArray out;
  for (const auto &t : ordered)
    out.append(t);
  return out;
}
QJsonArray sortedDevices(const QJsonArray &input, bool paired) {
  QList<QJsonObject> rows;
  for (const auto &v : input) {
    auto d = v.toObject();
    if (d["paired"].toBool() == paired && d["kind"] == "engine")
      rows.append(d);
  }
  QCollator c;
  c.setNumericMode(true);
  std::sort(rows.begin(), rows.end(),
            [&](const QJsonObject &a, const QJsonObject &b) {
              if (a["online"] != b["online"])
                return a["online"].toBool();
              const auto n =
                  c.compare(a["name"].toString(), b["name"].toString());
              return n == 0 ? a["id"].toString() < b["id"].toString() : n < 0;
            });
  QJsonArray out;
  for (const auto &r : rows)
    out.append(r);
  return out;
}
QJsonObject sendBatch(const QJsonObject &d, const QStringList &paths,
                      const QString &requestId) {
  if (!d["paired"].toBool() || !d["canReceive"].toBool() ||
      d["kind"] != "engine" || paths.isEmpty())
    return {};
  QJsonArray files;
  for (const auto &p : paths) {
    if (!QDir::isAbsolutePath(p))
      return {};
    files.append(QDir::cleanPath(p));
  }
  const QString key = requestId.isEmpty()
                          ? QUuid::createUuid().toString(QUuid::Id128)
                          : requestId;
  return {{"deviceId", d["id"]}, {"paths", files}, {"requestId", key}};
}
QString nativePackageFormat(const QString &osRelease) {
  QStringList families;
  for (const auto &line : osRelease.split('\n')) {
    if (!line.startsWith("ID=") && !line.startsWith("ID_LIKE="))
      continue;
    auto value = line.mid(line.indexOf('=') + 1).trimmed();
    value.remove('"');
    value.remove('\'');
    families.append(value.toLower().split(' ', Qt::SkipEmptyParts));
  }
  for (const auto &family : families) {
    if (family == "arch" || family == "manjaro")
      return "arch";
    if (family == "debian" || family == "ubuntu")
      return "deb";
    if (family == "fedora" || family == "rhel" || family == "suse" ||
        family.startsWith("opensuse"))
      return "rpm";
  }
  return {};
}
QString dataDirectory() {
  return QDir(QStandardPaths::writableLocation(
                  QStandardPaths::GenericConfigLocation))
      .filePath("AirLink");
}
QString downloadsDirectory() {
  return QStandardPaths::writableLocation(QStandardPaths::DownloadLocation);
}
QString redactedDiagnostic(QString v) {
  v.replace(QRegularExpression("[A-Fa-f0-9]{48,}"), "<redacted>");
  v.replace(QRegularExpression("(token|secret|password)[=: ]+[^\\s]+",
                               QRegularExpression::CaseInsensitiveOption),
            "<redacted>");
  return v.right(4096);
}
} // namespace AirLink
