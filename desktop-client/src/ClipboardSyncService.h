#pragma once

#include "Models.h"

#include <QObject>
#include <QQueue>
#include <QSet>

class ClipboardManager;
class CryptoManager;
class DeviceManager;
class HistoryRepository;
class SensitiveContentDetector;
class SettingsRepository;
class WebSocketClient;

class ClipboardSyncService final : public QObject {
    Q_OBJECT

public:
    ClipboardSyncService(ClipboardManager *clipboard, CryptoManager *crypto,
                         DeviceManager *devices, HistoryRepository *history,
                         SensitiveContentDetector *sensitive,
                         SettingsRepository *settings, WebSocketClient *websocket,
                         QObject *parent = nullptr);

    void setTargetDeviceIds(const QStringList &ids);
    [[nodiscard]] QStringList targetDeviceIds() const;
    void sendCurrentClipboard(bool allowSensitive = false);
    void sendText(const QString &text, bool allowSensitive = false);

signals:
    void sensitiveConfirmationRequired(const QString &text,
                                       const QStringList &reasons);
    void remoteTextReceived(const QString &text, const QString &sourceName,
                            bool sensitive);
    void statusMessage(const QString &message);
    void historyChanged();

private slots:
    void receiveEnvelope(const Envelope &envelope);

private:
    void rememberMessage(const QString &id);

    ClipboardManager *clipboard_;
    CryptoManager *crypto_;
    DeviceManager *devices_;
    HistoryRepository *history_;
    SensitiveContentDetector *sensitive_;
    SettingsRepository *settings_;
    WebSocketClient *websocket_;
    QStringList targets_;
    QSet<QString> recentMessages_;
    QQueue<QString> recentOrder_;
    QByteArray recentUploadHash_;
    qint64 recentUploadAt_{0};
};

