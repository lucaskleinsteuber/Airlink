#include "widgets.h"
#include "core.h"
#include <QDateTime>
#include <QDragEnterEvent>
#include <QFile>
#include <QHBoxLayout>
#include <QJsonArray>
#include <QLabel>
#include <QLocale>
#include <QMimeData>
#include <QMimeDatabase>
#include <QPainter>
#include <QPainterPath>
#include <QProgressBar>
#include <QPushButton>
#include <QTimer>
#include <QToolButton>
#include <QVBoxLayout>
#include <cmath>

namespace AirLink {
QLabel *label(const QString &value, QWidget *parent, bool heading) {
  return styledLabel(value, parent, heading ? 1.5 : 1.0, heading);
}
QLabel *styledLabel(const QString &value, QWidget *parent, double scale,
                    bool bold, bool secondary) {
  auto *v = new QLabel(value, parent);
  v->setWordWrap(true);
  v->setTextFormat(Qt::PlainText);
  auto f = v->font();
  if (scale != 1.0)
    f.setPointSizeF(f.pointSizeF() * scale);
  if (bold)
    f.setWeight(QFont::DemiBold);
  v->setFont(f);
  if (secondary) {
    auto p = v->palette();
    p.setColor(QPalette::WindowText, p.color(QPalette::PlaceholderText));
    v->setPalette(p);
  }
  return v;
}
QPushButton *button(const QString &label, QWidget *parent,
                    std::function<void()> callback) {
  auto *b = new QPushButton(label, parent);
  QObject::connect(b, &QPushButton::clicked, parent,
                   [callback = std::move(callback)] { callback(); });
  return b;
}
QIcon themedIcon(const QString &name) {
  if (QIcon::hasThemeIcon(name))
    return QIcon::fromTheme(name);
  const QString bundled = ":/airlink/icons/" + name + ".svg";
  if (QFile::exists(bundled))
    return QIcon(bundled);
  return QIcon::fromTheme(name, QIcon(":/airlink/icon.svg"));
}
QColor mix(const QColor &a, const QColor &b, double amount) {
  const double t = std::clamp(amount, 0.0, 1.0);
  return QColor::fromRgbF(
      static_cast<float>(a.redF() * (1 - t) + b.redF() * t),
      static_cast<float>(a.greenF() * (1 - t) + b.greenF() * t),
      static_cast<float>(a.blueF() * (1 - t) + b.blueF() * t));
}
QString desktopQuoted(QString value) {
  value.replace("\\", "\\\\");
  value.replace('"', "\\\"");
  value.replace('`', "\\`");
  value.replace('$', "\\$");
  value.replace('%', "%%");
  return '"' + value + '"';
}
QString relativeTime(const QString &iso, bool en) {
  auto when = QDateTime::fromString(iso, Qt::ISODateWithMs);
  if (!when.isValid())
    when = QDateTime::fromString(iso, Qt::ISODate);
  if (!when.isValid())
    return {};
  const qint64 s =
      std::max<qint64>(0, when.secsTo(QDateTime::currentDateTimeUtc()));
  if (s < 60)
    return en ? QStringLiteral("just now") : QStringLiteral("gerade eben");
  if (s < 3600)
    return en ? QString("%1 min ago").arg(s / 60)
              : QString("vor %1 Min.").arg(s / 60);
  if (s < 86400)
    return en ? QString("%1 h ago").arg(s / 3600)
              : QString("vor %1 Std.").arg(s / 3600);
  return QLocale(en ? QLocale::English : QLocale::German)
      .toString(when.toLocalTime().date(), QLocale::ShortFormat);
}

FlowLayout::FlowLayout(QWidget *parent, int spacing)
    : QLayout(parent), gap(spacing) {
  setContentsMargins(0, 0, 0, 0);
}
FlowLayout::~FlowLayout() { clear(); }
void FlowLayout::addItem(QLayoutItem *item) { items.append(item); }
int FlowLayout::count() const { return static_cast<int>(items.size()); }
QLayoutItem *FlowLayout::itemAt(int index) const {
  return items.value(index);
}
QLayoutItem *FlowLayout::takeAt(int index) {
  return index >= 0 && index < items.size() ? items.takeAt(index) : nullptr;
}
void FlowLayout::clear() {
  while (auto *item = takeAt(0)) {
    if (auto *w = item->widget()) {
      w->hide();
      w->deleteLater();
    }
    delete item;
  }
}
int FlowLayout::heightForWidth(int width) const {
  return arrange(QRect(0, 0, width, 0), false);
}
QSize FlowLayout::minimumSize() const {
  QSize size;
  for (auto *item : items)
    size = size.expandedTo(item->minimumSize());
  return size;
}
void FlowLayout::setGeometry(const QRect &rect) {
  QLayout::setGeometry(rect);
  arrange(rect, true);
}
int FlowLayout::arrange(const QRect &rect, bool apply) const {
  int x = rect.x(), y = rect.y(), line = 0;
  for (auto *item : items) {
    const auto hint = item->sizeHint();
    if (x + hint.width() > rect.right() + 1 && line > 0) {
      x = rect.x();
      y += line + gap;
      line = 0;
    }
    if (apply)
      item->setGeometry(QRect(QPoint(x, y), hint));
    x += hint.width() + gap;
    line = std::max(line, hint.height());
  }
  return y + line - rect.y();
}

DeviceTile::DeviceTile(Kind kind, QWidget *parent, int size)
    : QWidget(parent), tileKind(kind), diameter(size) {
  setAcceptDrops(kind == Kind::Paired);
  setFocusPolicy(Qt::StrongFocus);
  setCursor(Qt::PointingHandCursor);
  setAttribute(Qt::WA_Hover);
  spinner = new QTimer(this);
  spinner->setInterval(30);
  connect(spinner, &QTimer::timeout, this, [this] {
    spin = (spin + 9) % 360;
    update();
  });
}
QSize DeviceTile::sizeHint() const {
  const auto lines = fontMetrics().height();
  return {diameter + 58, diameter + 26 + lines * 3 + 8};
}
void DeviceTile::setContent(const QString &iconName, const QString &name,
                            const QString &state, double fraction,
                            bool working, bool enabled) {
  icon = themedIcon(iconName);
  title = name;
  status = state;
  progress = fraction;
  busy = working;
  reachable = enabled;
  setAccessibleName(name + ", " + state);
  if (busy && progress < 0)
    spinner->start();
  else
    spinner->stop();
  update();
}
void DeviceTile::paintEvent(QPaintEvent *) {
  QPainter p(this);
  p.setRenderHint(QPainter::Antialiasing);
  const auto pal = palette();
  const QColor accent = pal.color(QPalette::Highlight),
               text = pal.color(QPalette::WindowText),
               window = pal.color(QPalette::Window),
               muted = pal.color(QPalette::PlaceholderText);
  const QRectF circle((width() - diameter) / 2.0, 10, diameter, diameter);
  if (tileKind == Kind::Paired) {
    const double tint = !reachable ? 0.02 : targeted ? 0.32 : hovered ? 0.18 : 0.10;
    p.setPen(QPen(mix(window, text, 0.14), 1));
    p.setBrush(mix(pal.color(QPalette::Base), accent, tint));
    p.drawEllipse(circle);
    if (targeted) {
      p.setPen(QPen(accent, 3));
      p.setBrush(Qt::NoBrush);
      p.drawEllipse(circle.adjusted(1.5, 1.5, -1.5, -1.5));
    }
  } else {
    QPen dash(hovered ? accent : mix(window, text, 0.38), 1.5);
    dash.setDashPattern({4, 3});
    p.setPen(dash);
    p.setBrush(mix(window, text, 0.03));
    p.drawEllipse(circle.adjusted(1, 1, -1, -1));
  }
  const int iconSize = static_cast<int>(diameter * 0.48);
  const QRect iconRect(static_cast<int>(circle.center().x()) - iconSize / 2,
                       static_cast<int>(circle.center().y()) - iconSize / 2,
                       iconSize, iconSize);
  icon.paint(&p, iconRect, Qt::AlignCenter,
             reachable ? QIcon::Normal : QIcon::Disabled);
  const QRectF ring = circle.adjusted(-6, -6, 6, 6);
  if (progress >= 0) {
    p.setBrush(Qt::NoBrush);
    p.setPen(QPen(mix(window, accent, 0.22), 4));
    p.drawEllipse(ring);
    QPen arc(accent, 4);
    arc.setCapStyle(Qt::RoundCap);
    p.setPen(arc);
    p.drawArc(ring, 90 * 16,
              -static_cast<int>(360 * 16 * std::max(0.02, progress)));
  } else if (busy) {
    QPen arc(accent, 4);
    arc.setCapStyle(Qt::RoundCap);
    p.setPen(arc);
    p.setBrush(Qt::NoBrush);
    p.drawArc(ring, (90 - spin) * 16, -80 * 16);
  }
  if (hasFocus()) {
    QPen focus(accent, 2);
    focus.setStyle(Qt::DotLine);
    p.setPen(focus);
    p.setBrush(Qt::NoBrush);
    p.drawEllipse(ring.adjusted(-3, -3, 3, 3));
  }
  auto bold = font();
  bold.setWeight(QFont::DemiBold);
  p.setFont(bold);
  p.setPen(reachable ? text : muted);
  const QFontMetrics fm(bold);
  const int top = static_cast<int>(ring.bottom()) + 8;
  const QRect nameRect(2, top, width() - 4, fm.height() * 2);
  const QString shown = fm.elidedText(title, Qt::ElideRight, (width() - 4) * 2 - 8);
  p.drawText(nameRect, Qt::AlignHCenter | Qt::AlignTop | Qt::TextWordWrap,
             shown);
  const int lines = fm.boundingRect(nameRect, Qt::AlignHCenter | Qt::TextWordWrap, shown).height() > fm.height() ? 2 : 1;
  auto small = font();
  small.setPointSizeF(small.pointSizeF() * 0.9);
  p.setFont(small);
  p.setPen(busy ? accent : muted);
  const QFontMetrics sm(small);
  p.drawText(QRect(2, top + fm.height() * lines + 2, width() - 4, sm.height()),
             Qt::AlignHCenter | Qt::AlignTop,
             sm.elidedText(status, Qt::ElideRight, width() - 4));
}
void DeviceTile::enterEvent(QEnterEvent *) {
  hovered = true;
  update();
}
void DeviceTile::leaveEvent(QEvent *) {
  hovered = false;
  update();
}
void DeviceTile::mouseReleaseEvent(QMouseEvent *event) {
  if (event->button() == Qt::LeftButton && rect().contains(event->position().toPoint()))
    emit activated();
}
void DeviceTile::contextMenuEvent(QContextMenuEvent *event) {
  emit menuRequested(event->globalPos());
}
void DeviceTile::keyPressEvent(QKeyEvent *event) {
  if (event->key() == Qt::Key_Return || event->key() == Qt::Key_Enter ||
      event->key() == Qt::Key_Space)
    emit activated();
  else if (event->key() == Qt::Key_Menu)
    emit menuRequested(mapToGlobal(rect().center()));
  else
    QWidget::keyPressEvent(event);
}
void DeviceTile::dragEnterEvent(QDragEnterEvent *event) {
  if (tileKind == Kind::Paired && event->mimeData()->hasUrls()) {
    targeted = true;
    update();
    event->acceptProposedAction();
  }
}
void DeviceTile::dragLeaveEvent(QDragLeaveEvent *) {
  targeted = false;
  update();
}
void DeviceTile::dropEvent(QDropEvent *event) {
  targeted = false;
  update();
  QStringList paths;
  for (const auto &url : event->mimeData()->urls())
    if (url.isLocalFile())
      paths.append(url.toLocalFile());
  if (!paths.isEmpty()) {
    event->acceptProposedAction();
    emit filesDropped(paths);
  }
}

static QPixmap fileIcon(const QJsonObject &t, int size, const QColor &badge) {
  const auto files = t["files"].toArray();
  const auto first = files.isEmpty() ? QJsonObject{} : files.first().toObject();
  QIcon base;
  if (first["directory"].toBool())
    base = themedIcon("folder");
  else {
    const auto type = QMimeDatabase().mimeTypeForFile(
        first["path"].toString(), QMimeDatabase::MatchExtension);
    base = QIcon::fromTheme(type.iconName(),
                            QIcon::fromTheme(type.genericIconName(),
                                             themedIcon("text-x-generic")));
  }
  const qreal ratio = 2;
  QPixmap out(QSize(size + 6, size + 4) * ratio);
  out.setDevicePixelRatio(ratio);
  out.fill(Qt::transparent);
  QPainter p(&out);
  p.setRenderHint(QPainter::Antialiasing);
  base.paint(&p, QRect(0, 0, size, size));
  const QRectF dot(size - 10, size - 12, 15, 15);
  p.setPen(Qt::NoPen);
  p.setBrush(badge);
  p.drawEllipse(dot);
  QPen arrow(Qt::white, 1.6);
  arrow.setCapStyle(Qt::RoundCap);
  p.setPen(arrow);
  const QPointF c = dot.center();
  const bool up = t["direction"] == "send";
  p.drawLine(QPointF(c.x(), c.y() - 4), QPointF(c.x(), c.y() + 4));
  const double tip = up ? -4 : 4;
  p.drawLine(QPointF(c.x(), c.y() + tip), QPointF(c.x() - 3, c.y() + tip * 0.2));
  p.drawLine(QPointF(c.x(), c.y() + tip), QPointF(c.x() + 3, c.y() + tip * 0.2));
  return out;
}

TransferRow::TransferRow(QWidget *parent) : QWidget(parent) {
  auto *row = new QHBoxLayout(this);
  row->setContentsMargins(14, 9, 12, 9);
  row->setSpacing(12);
  icon = new QLabel(this);
  icon->setFixedSize(42, 38);
  row->addWidget(icon, 0, Qt::AlignVCenter);
  auto *texts = new QVBoxLayout;
  texts->setSpacing(3);
  title = styledLabel("", this, 1.0, true);
  title->setWordWrap(false);
  title->setTextFormat(Qt::PlainText);
  texts->addWidget(title);
  progress = new QProgressBar(this);
  progress->setRange(0, 1000);
  progress->setTextVisible(false);
  progress->setFixedHeight(6);
  texts->addWidget(progress);
  detail = styledLabel("", this, 0.9, false, true);
  texts->addWidget(detail);
  row->addLayout(texts, 1);
  pauseButton = new QToolButton(this);
  pauseButton->setAutoRaise(true);
  connect(pauseButton, &QToolButton::clicked, this, [this] {
    emit action(pausedState ? "resume" : "pause", transferId);
  });
  cancelButton = new QToolButton(this);
  cancelButton->setAutoRaise(true);
  cancelButton->setIcon(themedIcon("window-close"));
  connect(cancelButton, &QToolButton::clicked, this,
          [this] { emit action("cancel", transferId); });
  primary = new QPushButton(this);
  connect(primary, &QPushButton::clicked, this,
          [this] { emit action(primaryAction, transferId); });
  row->addWidget(pauseButton);
  row->addWidget(cancelButton);
  row->addWidget(primary);
}
void TransferRow::setTransfer(const QJsonObject &t, bool en) {
  const auto tr = [en](const char *de, const char *english) {
    return QString::fromUtf8(en ? english : de);
  };
  transferId = t["id"].toString();
  const auto state = t["state"].toString(), device = t["deviceName"].toString();
  const bool send = t["direction"] == "send";
  const auto files = t["files"].toArray();
  const QString key = t["direction"].toString() + '|' +
                      (files.isEmpty() ? QString{} : files.first().toObject()["path"].toString());
  if (key != iconKey) {
    iconKey = key;
    icon->setPixmap(fileIcon(t, 34, send ? palette().color(QPalette::Highlight)
                                         : QColor("#2E9E4F")));
  }
  title->setText(describeTransfer(t, en));
  const double fraction = transferFraction(t);
  const int percent = static_cast<int>(fraction * 100);
  const QString peer = send ? tr("An ", "To ") + device : tr("Von ", "From ") + device;
  QString text;
  if (state == "running") {
    text = peer + " · " + QString::number(percent) + " % · " +
           speed(t["bytesPerSecond"].toDouble());
    const auto left = remaining(t);
    if (left != QStringLiteral("—"))
      text += tr(" · noch ", " · ") + left + (en ? " left" : "");
  } else if (state == "paused")
    text = peer + " · " + (en ? QString("Paused at %1%").arg(percent)
                              : QString("Angehalten bei %1 %").arg(percent));
  else if (state == "offered")
    text = send ? peer + " · " + (en ? "Waiting for " + device + " to accept"
                                     : "Wartet, bis " + device + " annimmt")
                : peer + " · " + bytes(static_cast<qint64>(t["totalBytes"].toDouble()));
  else if (state == "queued" || state == "reconnecting" || state == "verifying")
    text = peer + " · " + (en ? QString("Waiting for %1 … (%2%)").arg(device).arg(percent)
                              : QString("Wartet auf %1 … (%2 %)").arg(device).arg(percent));
  else if (state == "failed") {
    const auto problem = t["problem"].toObject();
    text = !problem.isEmpty()
               ? problem[en ? "titleEN" : "titleDE"].toString() + ' ' +
                     problem[en ? "recoveryEN" : "recoveryDE"].toString()
               : peer + " · " + tr("Fehlgeschlagen", "Failed") +
                     (t["error"].toString().isEmpty() ? QString{} : ": " + t["error"].toString());
  } else if (state == "cancelled")
    text = peer + " · " + tr("Abgebrochen", "Cancelled");
  else
    text = peer + " · " + bytes(static_cast<qint64>(t["totalBytes"].toDouble())) +
           " · " + relativeTime(t["updatedAt"].toString(), en);
  detail->setText(text);
  auto pal = detail->palette();
  pal.setColor(QPalette::WindowText, state == "failed" ? QColor("#D2453C")
                                                       : palette().color(QPalette::PlaceholderText));
  detail->setPalette(pal);
  progress->setVisible(state == "running");
  progress->setValue(static_cast<int>(fraction * 1000));
  const bool moving = state == "running" || state == "queued" ||
                      state == "reconnecting" || state == "verifying";
  pausedState = state == "paused";
  pauseButton->setVisible(moving || pausedState);
  pauseButton->setIcon(themedIcon(pausedState ? "media-playback-start" : "media-playback-pause"));
  pauseButton->setToolTip(pausedState ? tr("Fortsetzen", "Resume") : tr("Anhalten", "Pause"));
  cancelButton->setVisible(moving || pausedState || (state == "offered" && send));
  cancelButton->setToolTip(tr("Abbrechen", "Cancel"));
  primaryAction.clear();
  if (state == "offered" && !send) {
    primaryAction = "accept";
    primary->setText(tr("Annehmen", "Accept"));
  } else if (state == "completed" && !send && t["hasContent"].toBool(true)) {
    primaryAction = "reveal";
    primary->setText(tr("Zeigen", "Show"));
    primary->setToolTip(tr("Im Dateimanager zeigen", "Show in the file manager"));
  } else if (state == "failed" && t["retryable"].toBool()) {
    primaryAction = "retry";
    primary->setText(tr("Erneut versuchen", "Try again"));
  }
  primary->setVisible(!primaryAction.isEmpty());
  setAccessibleName(title->text() + ", " + text);
}

Banner::Banner(const QString &iconName, const QString &heading,
               const QString &text, QWidget *parent)
    : QFrame(parent) {
  auto *row = new QHBoxLayout(this);
  row->setContentsMargins(16, 12, 14, 12);
  row->setSpacing(14);
  auto *picture = new QLabel(this);
  picture->setPixmap(themedIcon(iconName).pixmap(28, 28));
  row->addWidget(picture, 0, Qt::AlignVCenter);
  auto *texts = new QVBoxLayout;
  texts->setSpacing(2);
  texts->addWidget(styledLabel(heading, this, 1.0, true));
  if (!text.isEmpty()) {
    auto *body = styledLabel(text, this, 0.95, false, true);
    body->setMaximumHeight(body->fontMetrics().height() * 2 + 4);
    texts->addWidget(body);
  }
  row->addLayout(texts, 1);
  actionBox = new QHBoxLayout;
  actionBox->setSpacing(8);
  row->addLayout(actionBox);
}
void Banner::paintEvent(QPaintEvent *) {
  QPainter p(this);
  p.setRenderHint(QPainter::Antialiasing);
  const auto pal = palette();
  const auto accent = pal.color(QPalette::Highlight),
             window = pal.color(QPalette::Window);
  p.setPen(QPen(mix(window, accent, 0.45), 1));
  p.setBrush(mix(window, accent, 0.12));
  p.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 10, 10);
}

Card::Card(QWidget *parent) : QFrame(parent) {}
void Card::paintEvent(QPaintEvent *) {
  QPainter p(this);
  p.setRenderHint(QPainter::Antialiasing);
  const auto pal = palette();
  p.setPen(QPen(mix(pal.color(QPalette::Window), pal.color(QPalette::WindowText), 0.14), 1));
  p.setBrush(pal.color(QPalette::Base));
  p.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 10, 10);
}
} // namespace AirLink
