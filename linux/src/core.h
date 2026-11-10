#pragma once
#include <QJsonArray>
#include <QJsonObject>
#include <QStringList>

namespace AirLink {
struct SelectedSource {
  QString id, path;
};
class SourceSelection {
public:
  void append(const QStringList &paths);
  QStringList paths() const;
  QStringList identities() const;
  void acknowledge(const QStringList &ids);
  void clear() { entries.clear(); }
  bool empty() const { return entries.isEmpty(); }

private:
  QList<SelectedSource> entries;
};
class SettingsDraft {
public:
  QJsonObject baseline, values;
  void reset(const QJsonObject &value);
  void rebase(const QJsonObject &latest);
  QJsonObject merged(const QJsonObject &latest) const;
  bool dirty() const;
  QString validate() const;
};
bool terminal(const QString &state);
QString bytes(qint64 value);
QString speed(double value);
QString remaining(const QJsonObject &transfer);
QString transferTitle(const QJsonObject &transfer);
QString stateLabel(const QString &state, bool english);
QJsonArray visibleTransfers(const QJsonArray &transfers, const QString &device,
                            int recent = 3);
QJsonArray mergeHistory(const QJsonArray &history, const QJsonArray &summary);
QJsonArray sortedDevices(const QJsonArray &devices, bool paired);
QJsonObject sendBatch(const QJsonObject &device, const QStringList &paths,
                      const QString &requestId = {});
QString dataDirectory();
QString downloadsDirectory();
QString redactedDiagnostic(QString value);
} // namespace AirLink
