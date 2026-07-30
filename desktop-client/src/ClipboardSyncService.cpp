#include "ClipboardSyncService.h"

#include "ClipboardManager.h"
#include "CryptoManager.h"
#include "DeviceManager.h"
#include "HistoryRepository.h"
#include "SensitiveContentDetector.h"
#include "SettingsRepository.h"
#include "WebSocketClient.h"

#include <QDateTime>
#include <QUuid>

ClipboardSyncService::ClipboardSyncService(
    ClipboardManager *clipboard, CryptoManager *crypto, DeviceManager *devices,
    HistoryRepository *history, SensitiveContentDetector *sensitive,
    SettingsRepository *settings, WebSocketClient *websocket, QObject *parent)
    : QObject(parent),
      clipboard_(clipboard),
      crypto_(crypto),
      devices_(devices),
      history_(history),
      sensitive_(sensitive),
      settings_(settings),
      websocket_(websocket) {
    connect(clipboard_, &ClipboardManager::localTextChanged, this,
            [this](const QString &text) { sendText(text); });
    connect(websocket_, &WebSocketClient::envelopeReceived, this,
            &ClipboardSyncService::receiveEnvelope);
    connect(websocket_, &WebSocketClient::messageAcknowledged, this,
            [this](const QString &id) {
                history_->markSent(id);
                emit historyChanged();
            });
}

void ClipboardSyncService::setTargetDeviceIds(const QStringList &ids) {
    targets_ = ids;
}

QStringList ClipboardSyncService::targetDeviceIds() const { return targets_; }

void ClipboardSyncService::sendCurrentClipboard(bool allowSensitive) {
    sendText(clipboard_->currentText(), allowSensitive);
}

void ClipboardSyncService::sendText(const QString &text, bool allowSensitive) {
    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    if (!settings_->syncEnabled() || settings_->pausedUntil() > now) return;
    if (text.trimmed().isEmpty()) return;
    if (text.toUtf8().size() > settings_->maxTextLength()) {
        emit statusMessage(QStringLiteral("文本超过长度限制，未发送"));
        return;
    }
    if (targets_.isEmpty()) {
        emit statusMessage(QStringLiteral("请选择接收设备"));
        return;
    }
    const QByteArray hash = crypto_->contentHash(text);
    if (hash == recentUploadHash_ &&
        now - recentUploadAt_ < ClipboardManager::duplicateWindowMs()) {
        return;
    }

    const QStringList reasons = sensitive_->detect(text);
    if (!reasons.isEmpty() && !allowSensitive) {
        emit sensitiveConfirmationRequired(text, reasons);
        return;
    }

    int sentCount = 0;
    for (const QString &targetId : targets_) {
        const Device target = devices_->device(targetId);
        if (target.id.isEmpty() || target.revoked ||
            target.id == devices_->deviceId()) {
            continue;
        }
        QString error;
        const qint64 ttl = !reasons.isEmpty()
                               ? 60000
                               : static_cast<qint64>(settings_->offlineTtlSeconds()) * 1000;
        const bool offlineAllowed = settings_->offlineTtlSeconds() > 0;
        const Envelope envelope = crypto_->encryptText(
            text, devices_->deviceId(), target, qMax<qint64>(60000, ttl),
            offlineAllowed, &error);
        if (envelope.messageId.isEmpty()) {
            emit statusMessage(error);
            continue;
        }
        if (reasons.isEmpty()) {
            HistoryRecord record;
            record.messageId = envelope.messageId;
            record.content = text;
            record.contentHash = hash;
            record.sourceDevice = devices_->deviceId();
            record.targetDevice = target.id;
            record.createdAt = envelope.createdAt;
            record.receivedAt = envelope.createdAt;
            record.local = true;
            history_->add(record);
        }
        websocket_->sendEnvelope(envelope);
        rememberMessage(envelope.messageId);
        sentCount++;
    }
    if (sentCount > 0) {
        recentUploadHash_ = hash;
        recentUploadAt_ = now;
        emit statusMessage(QStringLiteral("已发送到 %1 台设备").arg(sentCount));
        emit historyChanged();
    }
}

void ClipboardSyncService::receiveEnvelope(const Envelope &envelope) {
    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    if (QUuid(envelope.messageId).isNull() ||
        envelope.recipientDeviceId != devices_->deviceId()) {
        emit statusMessage(QStringLiteral("收到无效路由消息"));
        return;
    }
    if (envelope.expiresAt <= now) {
        websocket_->sendAck(envelope.messageId, QStringLiteral("expired"));
        return;
    }
    if (recentMessages_.contains(envelope.messageId) ||
        history_->containsMessage(envelope.messageId)) {
        websocket_->sendAck(envelope.messageId);
        return;
    }
    const Device sender = devices_->device(envelope.senderDeviceId);
    if (sender.id.isEmpty() || sender.revoked) {
        emit statusMessage(QStringLiteral("拒绝未知或已撤销设备的消息"));
        return;
    }
    QString error;
    const QString text = crypto_->decryptText(envelope, sender, &error);
    if (!error.isEmpty()) {
        emit statusMessage(error);
        return;
    }
    const bool sensitive = sensitive_->isSensitive(text);
    if (!sensitive) {
        HistoryRecord record;
        record.messageId = envelope.messageId;
        record.content = text;
        record.contentHash = crypto_->contentHash(text);
        record.sourceDevice = sender.id;
        record.targetDevice = devices_->deviceId();
        record.createdAt = envelope.createdAt;
        record.receivedAt = now;
        record.read = true;
        record.sent = true;
        history_->add(record);
    }
    rememberMessage(envelope.messageId);
    websocket_->sendAck(envelope.messageId);
    if (settings_->autoWriteRemote() && !sensitive) {
        clipboard_->writeRemoteText(text, crypto_->contentHash(text));
    }
    emit remoteTextReceived(text, sender.name, sensitive);
    emit historyChanged();
}

void ClipboardSyncService::rememberMessage(const QString &id) {
    if (recentMessages_.contains(id)) return;
    recentMessages_.insert(id);
    recentOrder_.enqueue(id);
    while (recentOrder_.size() > 4096) {
        recentMessages_.remove(recentOrder_.dequeue());
    }
}
