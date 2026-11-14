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
QString deviceIconName(const QString &, const QString &form) {
  return form == "laptop" ? QStringLiteral("computer-laptop")
                          : QStringLiteral("computer");
}
QString describeTransfer(const QJsonObject &t, bool en) {
  const auto files = t["files"].toArray();
  const QString name = transferTitle(t).section('/', -1);
  if (files.isEmpty())
    return name;
  const auto first = files.first().toObject();
  if (first["directory"].toBool()) {
    const QString folder = first["path"].toString() + '/';
    for (int i = 1; i < files.size(); ++i)
      if (!files[i].toObject()["path"].toString().startsWith(folder))
        return en ? name + " and more" : name + " und weitere";
    return name;
  }
  const int count = t["fileCount"].toInt(files.size());
  if (count <= 1)
    return name;
  return en ? QString("%1 and %2 more").arg(name).arg(count - 1)
            : QString("%1 und %2 weitere").arg(name).arg(count - 1);
}
double transferFraction(const QJsonObject &t) {
  if (t["state"] == "completed")
    return 1;
  const double total = t["totalBytes"].toDouble();
  if (total <= 0)
    return 0;
  const double done =
      t["completedBytes"].toDouble() + std::max(0.0, t["skippedBytes"].toDouble());
  return std::clamp(done / total, 0.0, 1.0);
}
DeviceActivity deviceActivity(const QJsonObject &d, const QJsonArray &transfers,
                              const QJsonArray &pairings, bool preparing,
                              bool en) {
  const auto tr = [en](const char *de, const char *english) {
    return QString::fromUtf8(en ? english : de);
  };
  const QString id = d["id"].toString(), name = d["name"].toString();
  if (!d["paired"].toBool()) {
    for (const auto &v : pairings) {
      const auto p = v.toObject();
      const auto state = p["state"].toString();
      if ((state == "starting" || state == "comparing" ||
           state == "finishing") &&
          (p["peerId"].toString() == id || p["peerName"].toString() == name))
        return {tr("Verbindet …", "Connecting …"), -1, true};
    }
    if (d["outdated"].toBool())
      return {tr("Braucht Update", "Needs update")};
    return {tr("Zum Verbinden klicken", "Click to connect")};
  }
  if (preparing)
    return {tr("Wird vorbereitet …", "Preparing …"), -1, true};
  QList<QJsonObject> mine;
  for (const auto &v : transfers) {
    const auto t = v.toObject();
    if (t["deviceId"].toString() == id && !terminal(t["state"].toString()))
      mine.append(t);
  }
  if (!mine.isEmpty()) {
    double total = 0, done = 0;
    for (const auto &t : mine) {
      const double size = std::max(0.0, t["totalBytes"].toDouble());
      total += size;
      done += std::min(size, transferFraction(t) * size);
    }
    const double fraction = total > 0 ? std::min(1.0, done / total) : 0;
    const int percent = static_cast<int>(fraction * 100);
    for (const auto &t : mine)
      if (t["state"] == "running")
        return {t["direction"] == "send"
                    ? (en ? QString("Sending … %1%").arg(percent)
                          : QString("Sendet … %1 %").arg(percent))
                    : (en ? QString("Receiving … %1%").arg(percent)
                          : QString("Empfängt … %1 %").arg(percent)),
                fraction, true};
    const auto &first = mine.first();
    const auto state = first["state"].toString();
    if (state == "paused")
      return {tr("Angehalten", "Paused"), fraction};
    if (state == "offered")
      return first["direction"] == "send"
                 ? DeviceActivity{tr("Wartet auf Annahme", "Waiting for approval"), -1, true}
                 : DeviceActivity{tr("Möchte senden", "Wants to send")};
    return {d["online"].toBool() ? tr("Wartet …", "Waiting …")
                                 : tr("Wartet auf Verbindung", "Waiting to connect"),
            fraction > 0 ? fraction : -1, true};
  }
  return {d["online"].toBool() ? tr("Bereit", "Ready")
                               : tr("Nicht erreichbar", "Not reachable")};
}
QJsonArray recentTransfers(const QJsonArray &input, int limit) {
  QList<QJsonObject> active, done;
  for (const auto &v : input) {
    const auto t = v.toObject();
    (terminal(t["state"].toString()) ? done : active).append(t);
  }
  const auto newer = [](const QJsonObject &a, const QJsonObject &b) {
    const auto x = timestampKey(a["updatedAt"].toString()),
               y = timestampKey(b["updatedAt"].toString());
    return x == y ? a["id"].toString() > b["id"].toString() : x > y;
  };
  std::sort(active.begin(), active.end(), newer);
  std::sort(done.begin(), done.end(), newer);
  QJsonArray out;
  for (const auto &t : active)
    out.append(t);
  for (const auto &t : done) {
    if (out.size() >= std::max(limit, static_cast<int>(active.size())))
      break;
    out.append(t);
  }
  return out;
}
} // namespace AirLink
