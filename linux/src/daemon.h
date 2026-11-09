#pragma once
#include <QJsonObject>
#include <QObject>
#include <QProcess>
#include <QTimer>
#include <functional>

namespace AirLink {
class DaemonClient final : public QObject {
  Q_OBJECT
public:
  using Reply = std::function<void(QJsonValue, QString)>;
  explicit DaemonClient(QObject *parent = nullptr);
  void start(const QString &helper, const QString &directory,
             bool noDiscovery = false, const QString &receiveDirectory = {});
  void request(const QString &method, const QJsonObject &params, Reply reply);
  void refresh();
  void shutdown(std::function<void(bool)> done);
  bool ready() const { return available && !stopping; }
  const QJsonObject &status() const { return current; }
signals:
  void statusChanged();
  void failed(const QString &message);

private:
  struct Pending {
    Reply reply;
    qint64 deadline;
  };
  QProcess process;
  QTimer poll, deadlines;
  QHash<QString, Pending> pending;
  QByteArray input;
  QString diagnostics;
  QJsonObject current;
  bool available = false, stopping = false, refreshing = false;
  quint64 generation = 0;
  quint64 revision = 0;
  std::function<void(bool)> stopped;
  void consume();
  void fail(const QString &message);
};
} // namespace AirLink
