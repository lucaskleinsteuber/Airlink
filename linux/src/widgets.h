#pragma once
#include <QFrame>
#include <QIcon>
#include <QJsonObject>
#include <QLayout>
#include <QWidget>
#include <functional>

class QHBoxLayout;
class QLabel;
class QProgressBar;
class QPushButton;
class QTimer;
class QToolButton;

namespace AirLink {
QLabel *label(const QString &value, QWidget *parent, bool heading = false);
QLabel *styledLabel(const QString &value, QWidget *parent, double scale,
                    bool bold = false, bool secondary = false);
QPushButton *button(const QString &label, QWidget *parent,
                    std::function<void()> callback);
// A theme icon with a bundled fallback for desktops without that icon.
QIcon themedIcon(const QString &name);
QColor mix(const QColor &a, const QColor &b, double amount);
QString relativeTime(const QString &iso, bool english);
// Quotes a value for the Exec line of a .desktop file.
QString desktopQuoted(QString value);

// Lays out children left to right and wraps like text.
class FlowLayout final : public QLayout {
public:
  explicit FlowLayout(QWidget *parent = nullptr, int spacing = 12);
  ~FlowLayout() override;
  void addItem(QLayoutItem *item) override;
  int count() const override;
  QLayoutItem *itemAt(int index) const override;
  QLayoutItem *takeAt(int index) override;
  Qt::Orientations expandingDirections() const override { return {}; }
  bool hasHeightForWidth() const override { return true; }
  int heightForWidth(int width) const override;
  QSize minimumSize() const override;
  QSize sizeHint() const override { return minimumSize(); }
  void setGeometry(const QRect &rect) override;
  void clear();

private:
  QList<QLayoutItem *> items;
  int gap;
  int arrange(const QRect &rect, bool apply) const;
};

// A computer in the AirDrop-style grid: drop files on it or click it.
class DeviceTile final : public QWidget {
  Q_OBJECT
public:
  enum class Kind { Paired, Nearby, Add, Searching };
  explicit DeviceTile(Kind kind, QWidget *parent = nullptr, int diameter = 92);
  void setContent(const QString &iconName, const QString &title,
                  const QString &status, double progress, bool busy,
                  bool enabled);
  Kind kind() const { return tileKind; }
  QSize sizeHint() const override;

signals:
  void activated();
  void filesDropped(const QStringList &paths);
  void menuRequested(const QPoint &global);

protected:
  void paintEvent(QPaintEvent *event) override;
  void enterEvent(QEnterEvent *event) override;
  void leaveEvent(QEvent *event) override;
  void mouseReleaseEvent(QMouseEvent *event) override;
  void contextMenuEvent(QContextMenuEvent *event) override;
  void keyPressEvent(QKeyEvent *event) override;
  void dragEnterEvent(QDragEnterEvent *event) override;
  void dragLeaveEvent(QDragLeaveEvent *event) override;
  void dropEvent(QDropEvent *event) override;

private:
  Kind tileKind;
  int diameter;
  QIcon icon;
  QString title, status;
  double progress = -1;
  bool busy = false, reachable = true, hovered = false, targeted = false;
  int spin = 0;
  QTimer *spinner = nullptr;
};

// One transfer in "Recent" and in the history.
class TransferRow final : public QWidget {
  Q_OBJECT
public:
  explicit TransferRow(QWidget *parent = nullptr);
  void setTransfer(const QJsonObject &transfer, bool english);
  QString id() const { return transferId; }

signals:
  // pause, resume, cancel, accept, reveal, retry
  void action(const QString &kind, const QString &id);

private:
  QLabel *icon, *title, *detail;
  QProgressBar *progress;
  QToolButton *pauseButton, *cancelButton;
  QPushButton *primary;
  QString transferId, primaryAction, iconKey;
  bool pausedState = false;
};

// A rounded notice with an icon, text and actions on the right.
class Banner final : public QFrame {
public:
  Banner(const QString &iconName, const QString &title, const QString &text,
         QWidget *parent = nullptr);
  QHBoxLayout *actions() const { return actionBox; }

protected:
  void paintEvent(QPaintEvent *event) override;

private:
  QHBoxLayout *actionBox;
};

// A rounded group background like a card.
class Card final : public QFrame {
public:
  explicit Card(QWidget *parent = nullptr);

protected:
  void paintEvent(QPaintEvent *event) override;
};
} // namespace AirLink
