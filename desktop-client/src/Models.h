#pragma once

#include <QByteArray>
#include <QString>
#include <QVector>

struct Device {
    QString id;
    QString name;
    QString platform;
    QByteArray x25519PublicKey;
    QByteArray ed25519PublicKey;
    bool online{false};
    bool revoked{false};
};

struct TokenPair {
    QString accessToken;
    QString refreshToken;
    qint64 expiresAt{0};

    [[nodiscard]] bool isValid() const {
        return !accessToken.isEmpty() && !refreshToken.isEmpty();
    }
};

struct Envelope {
    int version{1};
    QString type{QStringLiteral("clipboard_text")};
    QString messageId;
    QString senderDeviceId;
    QString recipientDeviceId;
    qint64 createdAt{0};
    qint64 expiresAt{0};
    QByteArray nonce;
    QByteArray ciphertext;
    QByteArray signature;
    qint64 sequence{0};
    bool offlineAllowed{true};
};

struct HistoryRecord {
    qint64 id{0};
    QString messageId;
    QString content;
    QByteArray contentHash;
    QString sourceDevice;
    QString targetDevice;
    qint64 createdAt{0};
    qint64 receivedAt{0};
    bool favorite{false};
    bool read{false};
    bool sent{false};
    bool local{false};
    bool expired{false};
};

