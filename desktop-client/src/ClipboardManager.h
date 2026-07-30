#pragma once

#include <QByteArray>
#include <QObject>
#include <QTimer>

class QClipboard;

class ClipboardManager final : public QObject {
    Q_OBJECT

public:
    explicit ClipboardManager(QObject *parent = nullptr);
    static constexpr qint64 duplicateWindowMs() { return 15000; }
    [[nodiscard]] QString currentText() const;
    void writeRemoteText(const QString &text, const QByteArray &hash);

signals:
    void localTextChanged(const QString &text);

private slots:
    void onClipboardChanged();

private:
    QClipboard *clipboard_;
    QByteArray suppressedHash_;
    qint64 suppressUntil_{0};
    QByteArray lastEmittedHash_;
    qint64 lastEmittedAt_{0};
    qint64 lastLocalChangeAt_{0};
};
