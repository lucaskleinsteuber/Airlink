#include "core.h"
#include "daemon.h"
#include "updates.h"
#include <QCoreApplication>
#include <QCryptographicHash>
#include <QFile>
#include <QJsonDocument>
#include <QSignalSpy>
#include <QTemporaryDir>
#include <QtTest>
#include <openssl/evp.h>

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
  void historyMergesActualChangesAndAllActiveRows() {
    QJsonArray history{QJsonObject{{"id", "old"}, {"state", "completed"}},
                       QJsonObject{{"id", "changed"}, {"state", "completed"}}};
    QJsonArray summary{QJsonObject{{"id", "new"}, {"state", "running"}},
                       QJsonObject{{"id", "changed"}, {"state", "paused"}}};
    auto merged = mergeHistory(history, summary);
    QCOMPARE(merged.size(), 3);
    int active = 0;
    for (const auto &v : merged)
      if (!terminal(v.toObject()["state"].toString()))
        ++active;
    QCOMPARE(active, 2);
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
  void signedBetaFeedBindsArchitectureVersionAndExactFile() {
    QByteArray seed(32, 'x');
    EVP_PKEY *key = EVP_PKEY_new_raw_private_key(
        EVP_PKEY_ED25519, nullptr,
        reinterpret_cast<const unsigned char *>(seed.constData()), 32);
    QVERIFY(key);
    QByteArray publicKey(32, 0);
    size_t size = 32;
    QVERIFY(EVP_PKEY_get_raw_public_key(
                key, reinterpret_cast<unsigned char *>(publicKey.data()),
                &size) == 1);
    auto sign = [key](const QByteArray &body) {
      EVP_MD_CTX *ctx = EVP_MD_CTX_new();
      QByteArray sig(64, 0);
      size_t length = 64;
      const bool ok =
          ctx && EVP_DigestSignInit(ctx, nullptr, nullptr, nullptr, key) == 1 &&
          EVP_DigestSign(
              ctx, reinterpret_cast<unsigned char *>(sig.data()), &length,
              reinterpret_cast<const unsigned char *>(body.constData()),
              body.size()) == 1;
      EVP_MD_CTX_free(ctx);
      return ok ? sig : QByteArray{};
    };
    const QByteArray payload("exact signed package bytes");
    QJsonArray packages;
    for (const auto &entry :
         QList<QPair<QString, QString>>{{"appimage", ".AppImage"},
                                        {"deb", ".deb"},
                                        {"rpm", ".rpm"},
                                        {"arch", ".pkg.tar.zst"}}) {
      const auto file = "AirLink-0.12.0-beta.2-linux-x64" + entry.second;
      packages.append(QJsonObject{
          {"type", entry.first},
          {"file", file},
          {"url", "https://github.com/lucaskleinsteuber/Airlink/releases/"
                  "download/v0.12.0-beta.2/" +
                      file},
          {"length", payload.size()},
          {"sha256",
           QString::fromLatin1(
               QCryptographicHash::hash(payload, QCryptographicHash::Sha256)
                   .toHex())},
          {"ed25519Signature", QString::fromLatin1(sign(payload).toBase64())}});
    }
    QJsonObject manifest{{"schemaVersion", 1},         {"channel", "beta"},
                         {"platform", "linux"},        {"architecture", "x64"},
                         {"version", "0.12.0-beta.2"}, {"packages", packages}};
    auto body = QJsonDocument(manifest).toJson();
    UpdateInfo info;
    QString error;
    QVERIFY2(
        parseUpdate(body, sign(body), publicKey, "0.12.0-beta.1", info, error),
        qPrintable(error));
    QVERIFY(info.available);
    QTemporaryDir dir;
    QVERIFY(dir.isValid());
    QFile file(dir.path() + "/package");
    QVERIFY(file.open(QIODevice::WriteOnly));
    QCOMPARE(file.write(payload), payload.size());
    file.close();
    QVERIFY2(verifyUpdateFile(file.fileName(), info.packages["appimage"],
                              publicKey, error),
             qPrintable(error));
    QVERIFY(file.open(QIODevice::WriteOnly));
    file.write("modified");
    file.close();
    QVERIFY(!verifyUpdateFile(file.fileName(), info.packages["appimage"],
                              publicKey, error));
    QVERIFY(parseUpdate(body, sign(body), publicKey, "0.12.0-beta.10", info,
                        error));
    QVERIFY(!info.available);
    manifest["architecture"] = "arm64";
    body = QJsonDocument(manifest).toJson();
    QVERIFY(!parseUpdate(body, sign(body), publicKey, "0.11.0", info, error));
    EVP_PKEY_free(key);
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
