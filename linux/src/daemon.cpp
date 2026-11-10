#include "daemon.h"
#include "core.h"
#include <QDateTime>
#include <QJsonDocument>
#include <QSet>
#include <QUuid>
#include <limits>

namespace AirLink {
DaemonClient::DaemonClient(QObject *parent) : QObject(parent) {
  process.setProcessChannelMode(QProcess::SeparateChannels);
  connect(&process, &QProcess::readyReadStandardOutput, this,
          &DaemonClient::consume);
  connect(&process, &QProcess::readyReadStandardError, this, [this] {
    diagnostics = redactedDiagnostic(
        diagnostics + QString::fromUtf8(process.readAllStandardError()));
  });
  connect(&process, &QProcess::started, this, [this] {
    available = true;
    refresh();
    poll.start(1000);
  });
  connect(&process, &QProcess::errorOccurred, this,
          [this](QProcess::ProcessError e) {
            if (e == QProcess::FailedToStart)
              fail("The AirLink service could not start: " +
                   process.errorString());
          });
  connect(&process, qOverload<int, QProcess::ExitStatus>(&QProcess::finished),
          this, [this](int, QProcess::ExitStatus) {
            const bool expected = stopping;
            available = false;
            poll.stop();
            refreshing = false;
            auto callback = std::move(stopped);
            stopping = false;
            fail(expected ? QStringLiteral("AirLink stopped.")
                          : QStringLiteral("The AirLink service stopped. ") +
                                diagnostics);
            if (callback)
              callback(true);
          });
  connect(&poll, &QTimer::timeout, this, &DaemonClient::refresh);
  deadlines.setInterval(500);
  deadlines.start();
  connect(&deadlines, &QTimer::timeout, this, [this] {
    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    const auto keys = pending.keys();
    for (const auto &id : keys) {
      auto it = pending.find(id);
      if (it != pending.end() && it->deadline < now) {
        auto cb = std::move(it->reply);
        pending.erase(it);
        cb({}, "The AirLink service did not respond. Retry the action.");
      }
    }
  });
}
void DaemonClient::start(const QString &helper, const QString &directory,
                         bool noDiscovery, const QString &receiveDirectory) {
  if (process.state() != QProcess::NotRunning)
    return;
  ++generation;
  stopping = false;
  available = false;
  input.clear();
  diagnostics.clear();
  QStringList args{"--stdio", "--data-dir", directory};
  if (noDiscovery)
    args.append("--no-discovery");
  if (!receiveDirectory.isEmpty())
    args.append({"--receive-dir", receiveDirectory});
  process.start(helper, args, QIODevice::ReadWrite);
}
void DaemonClient::request(const QString &method, const QJsonObject &params,
                           Reply callback) {
  if (!available || process.state() != QProcess::Running ||
      (stopping && method != "shutdown")) {
    callback({}, "AirLink is not ready.");
    return;
  }
  if (pending.size() >= 128 || process.bytesToWrite() > 2 * 1024 * 1024) {
    callback({}, "AirLink is busy. Please retry.");
    return;
  }
  const QString id = QUuid::createUuid().toString(QUuid::Id128);
  static const QSet<QString> reads{
      "status",         "status.summary", "preparation.status",
      "pairing.status", "history.page",   "history.list",
      "transfer.files", "transfer.get",   "transfer.received",
      "share.list",     "clipboard.list", "relay.list",
      "statistics.get"};
  if (!reads.contains(method))
    ++revision;
  const QByteArray line =
      QJsonDocument(
          QJsonObject{{"id", id}, {"method", method}, {"params", params}})
          .toJson(QJsonDocument::Compact) +
      '\n';
  if (line.size() > 2 * 1024 * 1024) {
    callback(
        {},
        "This selection has too many paths. Select its parent folder instead.");
    return;
  }
  const bool preparing = method == "share.create" || method == "offer" ||
                         method == "duplicate.resolve";
  pending.insert(
      id, Pending{std::move(callback),
                  preparing ? std::numeric_limits<qint64>::max()
                            : QDateTime::currentMSecsSinceEpoch() + 20000});
  if (process.write(line) != line.size())
    fail("Could not send a command to the AirLink service.");
}
void DaemonClient::consume() {
  input += process.readAllStandardOutput();
  while (true) {
    const auto end = input.indexOf('\n');
    if (end < 0) {
      if (input.size() > 16 * 1024 * 1024)
        fail("The AirLink response was too large.");
      return;
    }
    if (end > 16 * 1024 * 1024) {
      fail("The AirLink response was too large.");
      return;
    }
    const auto line = input.left(end);
    input.remove(0, end + 1);
    QJsonParseError error;
    const auto object = QJsonDocument::fromJson(line, &error).object();
    if (error.error != QJsonParseError::NoError || object.isEmpty()) {
      fail("The AirLink service returned an invalid response.");
      return;
    }
    const auto id = object["id"].toString();
    auto it = pending.find(id);
    if (it == pending.end())
      continue;
    auto callback = std::move(it->reply);
    pending.erase(it);
    callback(object["result"], object["error"].toString());
  }
}
void DaemonClient::refresh() {
  if (!ready() || refreshing)
    return;
  refreshing = true;
  const auto session = generation, requestedRevision = revision;
  request("status.summary", {},
          [this, session, requestedRevision](QJsonValue value, QString error) {
            if (session != generation)
              return;
            refreshing = false;
            if (requestedRevision != revision) {
              refresh();
              return;
            }
            if (!error.isEmpty()) {
              fail(error);
              return;
            }
            if (!value.isObject() ||
                value.toObject()["id"].toString().isEmpty()) {
              fail("The AirLink status response is invalid.");
              return;
            }
            const auto next = value.toObject();
            if (next != current) {
              current = next;
              emit statusChanged();
            }
          });
}
void DaemonClient::fail(const QString &message) {
  available = false;
  refreshing = false;
  poll.stop();
  auto callbacks = std::move(pending);
  pending.clear();
  for (auto &p : callbacks)
    p.reply({}, message);
  if (!stopping)
    emit failed(message);
}
void DaemonClient::shutdown(std::function<void(bool)> done) {
  if (process.state() == QProcess::NotRunning) {
    done(true);
    return;
  }
  if (stopping) {
    done(false);
    return;
  }
  stopping = true;
  stopped = std::move(done);
  poll.stop();
  const auto session = generation;
  request("shutdown", {}, [this](QJsonValue, QString error) {
    if (error.isEmpty())
      process.closeWriteChannel();
  });
  process.closeWriteChannel();
  QTimer::singleShot(12000, this, [this, session] {
    if (generation == session && stopping) {
      stopping = false;
      auto cb = std::move(stopped);
      if (cb)
        cb(false);
    }
  });
}
} // namespace AirLink
