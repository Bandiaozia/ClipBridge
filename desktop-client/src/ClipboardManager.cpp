#include "ClipboardManager.h"

#include <QApplication>
#include <QClipboard>
#include <QCryptographicHash>
#include <QDateTime>
#include <QMimeData>

ClipboardManager::ClipboardManager(QObject *parent)
    : QObject(parent), clipboard_(QApplication::clipboard()) {
    connect(clipboard_, &QClipboard::dataChanged, this,
            &ClipboardManager::onClipboardChanged);
}

QString ClipboardManager::currentText() const {
    return clipboard_->text(QClipboard::Clipboard);
}

void ClipboardManager::writeRemoteText(const QString &text, const QByteArray &hash) {
    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    // 本机刚发生复制时，迟到的远端事件（尤其是厂商系统发出的旧缓存）
    // 不应覆盖用户手里的新内容。消息仍会被 ACK 和保存历史，只跳过自动写入。
    if (now - lastLocalChangeAt_ < 1500) return;
    // 某些桌面环境会为一次 setText 触发多次信号，使用摘要和时间窗共同抑制回环。
    suppressedHash_ = hash;
    suppressUntil_ = now + duplicateWindowMs();
    clipboard_->setText(text, QClipboard::Clipboard);
}

void ClipboardManager::onClipboardChanged() {
    const QString text = currentText();
    const QByteArray hash =
        QCryptographicHash::hash(text.toUtf8(), QCryptographicHash::Sha256);
    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    if (now <= suppressUntil_ && hash == suppressedHash_) return;
    if (hash == lastEmittedHash_ &&
        now - lastEmittedAt_ < duplicateWindowMs()) {
        return;
    }
    lastEmittedHash_ = hash;
    lastEmittedAt_ = now;
    lastLocalChangeAt_ = now;
    emit localTextChanged(text);
}
