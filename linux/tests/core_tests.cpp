#include "core.h"
#include "daemon.h"
#include "updates.h"
#include <QCoreApplication>
#include <QFile>
#include <QJsonDocument>
#include <QSignalSpy>
#include <QTemporaryDir>
#include <QtTest>

using namespace AirLink;
class CoreTests final : public QObject {
  Q_OBJECT
private slots:
  void settingsPreserveDraftAndUnknownFields() {
    SettingsDraft draft;
    QJsonObject initial{
        {"name", "Linux"},
        {"receiveDirectory", "/tmp/receive"},
        {"organization",
         QJsonObject{{"detectDuplicates", false}, {"rules", QJsonArray{}}}},
        {"unknownFuture", true}};
    draft.reset(initial);
    draft.values["name"] = "My Arch laptop";
    auto latest = initial;
    latest["receiveDirectory"] = "/tmp/new";
    auto org = latest["organization"].toObject();
    org["rules"] = QJsonArray{QJsonObject{{"id", "rule"},
                                          {"extensions", QJsonArray{"pdf"}},
                                          {"folder", "Documents"}}};
    latest["organization"] = org;
    draft.rebase(latest);
    QCOMPARE(draft.values["name"].toString(), "My Arch laptop");
    QCOMPARE(draft.values["receiveDirectory"].toString(), "/tmp/new");
    QVERIFY(draft.values["unknownFuture"].toBool());
    QCOMPARE(draft.values["organization"], latest["organization"]);
    QVERIFY(draft.dirty());
    QVERIFY(draft.validate().isEmpty());
    draft.values["name"] = "";
    draft.rebase(initial);
    QVERIFY(draft.values["name"].toString().isEmpty());
    QCOMPARE(draft.validate(), "name");
  }
  void directBatchCapturesRecipientAndSources() {
    QJsonObject device{{"id", "arch"},
                       {"paired", true},
                       {"kind", "engine"},
                       {"canReceive", true}};
    QStringList paths{"/tmp/original"};
    const auto batch = sendBatch(device, paths);
    device["id"] = "different";
    paths.append("/tmp/later");
    QCOMPARE(batch["deviceId"].toString(), "arch");
    QCOMPARE(batch["paths"].toArray().size(), 1);
    QCOMPARE(batch["requestId"].toString().size(), 32);
    QVERIFY(sendBatch(device, {"relative"}).isEmpty());
    device["canReceive"] = false;
    QVERIFY(sendBatch(device, paths).isEmpty());
  }
  void acknowledgedSelectionKeepsLaterReselection() {
    SourceSelection source;
    source.append({"/tmp/original"});
    const auto submitted = source.identities();
    source.append({"/tmp/original", "/tmp/later"});
    source.acknowledge(submitted);
    QCOMPARE(source.paths(), QStringList({"/tmp/original", "/tmp/later"}));
  }
  void transferFilteringKeepsActiveAndThreeResults() {
    QJsonArray list;
    for (int i = 0; i < 8; ++i)
      list.append(QJsonObject{{"id", QString::number(i)},
                              {"deviceId", i == 7 ? "other" : "chosen"},
                              {"state", i == 0 ? "paused" : "completed"},
                              {"updatedAt", QString::number(i)}});
    const auto shown = visibleTransfers(list, "chosen");
    QCOMPARE(shown.size(), 4);
    QCOMPARE(shown.first().toObject()["state"].toString(), "paused");
    QCOMPARE(visibleTransfers(list, "none").size(), 0);
  }
  void betaVersionsSortNumerically() {
    bool valid = false;
    QVERIFY(compareVersions("0.12.0-beta.10", "0.12.0-beta.2", &valid) > 0);
    QVERIFY(valid);
    QVERIFY(compareVersions("0.12.0", "0.12.0-beta.99") > 0);
    QVERIFY(compareVersions("0.12.0-beta.1", "0.11.0") > 0);
    QCOMPARE(compareVersions("0.12.0-beta.1", "0.12.0-beta.1"), 0);
    compareVersions("0.12.0-beta.01", "0.11.0", &valid);
    QVERIFY(!valid);
  }
  void badUpdateSignaturesFailBeforeParsing() {
    UpdateInfo info;
    QString error;
    QVERIFY(!parseUpdate("{}", QByteArray(64, 'x'), QByteArray(32, 'x'),
                         "0.11.0", info, error));
    QVERIFY(!error.isEmpty());
    QVERIFY(!info.available);
    QVERIFY(
        !verifyEd25519("message", QByteArray(63, 'x'), QByteArray(32, 'x')));
  }
  void diagnosticsDoNotExposeCredentials() {
    const auto value =
        redactedDiagnostic("token=very-secret " + QString(48, 'a'));
    QVERIFY(!value.contains("very-secret"));
    QVERIFY(!value.contains(QString(48, 'a')));
  }
  void actualHelperStatusAndCleanShutdown() {
    const auto helper = qEnvironmentVariable("AIRLINK_TEST_HELPER");
    if (helper.isEmpty())
      QSKIP("Set AIRLINK_TEST_HELPER to run the actual service integration.");
    QTemporaryDir dir;
    QVERIFY(dir.isValid());
    DaemonClient client;
    QSignalSpy changed(&client, &DaemonClient::statusChanged);
    client.start(helper, dir.path() + "/state", true, dir.path() + "/receive");
    QTRY_VERIFY_WITH_TIMEOUT(!changed.isEmpty(), 10000);
    QVERIFY(client.ready());
    QVERIFY(client.status()["summary"].toBool());
    bool stopped = false, clean = false;
    client.shutdown([&](bool result) {
      clean = result;
      stopped = true;
    });
    QTRY_VERIFY_WITH_TIMEOUT(stopped, 15000);
    QVERIFY(clean);
  }
};
QTEST_GUILESS_MAIN(CoreTests)
#include "core_tests.moc"
