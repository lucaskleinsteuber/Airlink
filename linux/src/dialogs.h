#pragma once
#include <QDialog>
#include <QJsonArray>
#include <QJsonObject>

class QCheckBox;
class QLabel;
class QLineEdit;
class QPushButton;
class QVBoxLayout;

namespace AirLink {
// Both computers show the same code; each side confirms that it matches.
class PairingDialog final : public QDialog {
  Q_OBJECT
public:
  PairingDialog(const QString &id, QWidget *parent);
  QString id() const { return sessionId; }
  void refresh(const QJsonObject &session, bool english);

signals:
  void confirmRequested(const QString &id, bool automatic);
  void cancelRequested(const QString &id);

private:
  QString sessionId;
  QLabel *ownIcon, *peerIcon, *link, *title, *message, *code, *hint;
  QCheckBox *automatic;
  QPushButton *cancel, *confirm, *closeButton;
  bool confirming = false;
};

// How to add a computer, the computers nearby and the address fallback.
class AddDeviceDialog final : public QDialog {
  Q_OBJECT
public:
  AddDeviceDialog(bool byAddress, bool english, QWidget *parent);
  void setNearby(const QJsonArray &devices);
  void setOwnAddress(const QString &address);
  void showIssue(const QString &text);

signals:
  void connectDevice(const QJsonObject &device);
  void connectAddress(const QString &address);

private:
  bool en;
  QVBoxLayout *nearbyList;
  QLabel *issue, *own;
  QLineEdit *address;
  QString nearbyKey;
  QString tr2(const char *de, const char *english) const;
};
} // namespace AirLink
