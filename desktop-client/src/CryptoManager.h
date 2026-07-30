#pragma once

#include "Models.h"
#include "SecureStorage.h"

#include <QObject>

class CryptoManager final : public QObject {
    Q_OBJECT

public:
    explicit CryptoManager(QObject *parent = nullptr);
    ~CryptoManager() override;

    bool initialize(QString *error = nullptr);
    [[nodiscard]] QByteArray x25519PublicKey() const;
    [[nodiscard]] QByteArray ed25519PublicKey() const;
    [[nodiscard]] QByteArray contentHash(const QString &text) const;
    [[nodiscard]] Envelope encryptText(const QString &text, const QString &senderId,
                                       const Device &recipient, qint64 ttlMs,
                                       bool offlineAllowed, QString *error = nullptr) const;
    [[nodiscard]] QString decryptText(const Envelope &envelope, const Device &sender,
                                      QString *error = nullptr) const;

    static QByteArray toBase64Url(const QByteArray &value);
    static QByteArray fromBase64Url(const QString &value);

private:
    [[nodiscard]] QByteArray directionKey(const QByteArray &peerPublic,
                                          const QString &senderId,
                                          const QString &recipientId,
                                          QString *error) const;
    [[nodiscard]] static QByteArray aad(const Envelope &envelope);
    [[nodiscard]] static QByteArray hkdfSha256(const QByteArray &secret,
                                               const QByteArray &salt,
                                               const QByteArray &info);

    SecureStorage storage_;
    QByteArray xPublic_;
    QByteArray xPrivate_;
    QByteArray signPublic_;
    QByteArray signPrivate_;
};

