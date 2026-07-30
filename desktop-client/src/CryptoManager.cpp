#include "CryptoManager.h"

#include <QCryptographicHash>
#include <QDateTime>
#include <QJsonDocument>
#include <QJsonObject>
#include <QUuid>

#include <algorithm>
#include <sodium.h>

namespace {
QByteArray hmacSha256(const QByteArray &key, const QByteArray &data) {
    QByteArray output(crypto_auth_hmacsha256_BYTES, Qt::Uninitialized);
    crypto_auth_hmacsha256_state state;
    crypto_auth_hmacsha256_init(
        &state, reinterpret_cast<const unsigned char *>(key.constData()),
        static_cast<size_t>(key.size()));
    crypto_auth_hmacsha256_update(
        &state, reinterpret_cast<const unsigned char *>(data.constData()),
        static_cast<unsigned long long>(data.size()));
    crypto_auth_hmacsha256_final(
        &state, reinterpret_cast<unsigned char *>(output.data()));
    return output;
}
}

CryptoManager::CryptoManager(QObject *parent) : QObject(parent) {
    if (sodium_init() < 0) {
        qFatal("libsodium 初始化失败");
    }
}

CryptoManager::~CryptoManager() {
    sodium_memzero(xPrivate_.data(), static_cast<size_t>(xPrivate_.size()));
    sodium_memzero(signPrivate_.data(), static_cast<size_t>(signPrivate_.size()));
}

bool CryptoManager::initialize(QString *error) {
    const QByteArray stored = storage_.load(QStringLiteral("identity-v1"), error);
    if (!stored.isEmpty()) {
        const QJsonObject object = QJsonDocument::fromJson(stored).object();
        xPublic_ = fromBase64Url(object.value(QStringLiteral("x_public")).toString());
        xPrivate_ = fromBase64Url(object.value(QStringLiteral("x_private")).toString());
        signPublic_ = fromBase64Url(object.value(QStringLiteral("sign_public")).toString());
        signPrivate_ = fromBase64Url(object.value(QStringLiteral("sign_private")).toString());
        if (xPublic_.size() == crypto_scalarmult_curve25519_BYTES &&
            xPrivate_.size() == crypto_scalarmult_curve25519_SCALARBYTES &&
            signPublic_.size() == crypto_sign_PUBLICKEYBYTES &&
            signPrivate_.size() == crypto_sign_SECRETKEYBYTES) {
            return true;
        }
        if (error) {
            *error = QStringLiteral("本机身份密钥长度无效");
        }
        return false;
    }

    xPublic_.resize(crypto_kx_PUBLICKEYBYTES);
    xPrivate_.resize(crypto_kx_SECRETKEYBYTES);
    signPublic_.resize(crypto_sign_PUBLICKEYBYTES);
    signPrivate_.resize(crypto_sign_SECRETKEYBYTES);
    crypto_kx_keypair(reinterpret_cast<unsigned char *>(xPublic_.data()),
                      reinterpret_cast<unsigned char *>(xPrivate_.data()));
    crypto_sign_keypair(reinterpret_cast<unsigned char *>(signPublic_.data()),
                        reinterpret_cast<unsigned char *>(signPrivate_.data()));
    const QJsonObject object{
        {QStringLiteral("x_public"), QString::fromLatin1(toBase64Url(xPublic_))},
        {QStringLiteral("x_private"), QString::fromLatin1(toBase64Url(xPrivate_))},
        {QStringLiteral("sign_public"), QString::fromLatin1(toBase64Url(signPublic_))},
        {QStringLiteral("sign_private"), QString::fromLatin1(toBase64Url(signPrivate_))},
    };
    return storage_.save(QStringLiteral("identity-v1"),
                         QJsonDocument(object).toJson(QJsonDocument::Compact), error);
}

QByteArray CryptoManager::x25519PublicKey() const { return xPublic_; }
QByteArray CryptoManager::ed25519PublicKey() const { return signPublic_; }

QByteArray CryptoManager::contentHash(const QString &text) const {
    return QCryptographicHash::hash(text.toUtf8(), QCryptographicHash::Sha256);
}

QByteArray CryptoManager::toBase64Url(const QByteArray &value) {
    return value.toBase64(QByteArray::Base64UrlEncoding |
                          QByteArray::OmitTrailingEquals);
}

QByteArray CryptoManager::fromBase64Url(const QString &value) {
    return QByteArray::fromBase64(value.toLatin1(), QByteArray::Base64UrlEncoding);
}

QByteArray CryptoManager::aad(const Envelope &e) {
    return QByteArray::number(e.version) + '\n' + e.messageId.toUtf8() + '\n' +
           e.senderDeviceId.toUtf8() + '\n' + e.recipientDeviceId.toUtf8() + '\n' +
           e.type.toUtf8() + '\n' + QByteArray::number(e.createdAt) + '\n' +
           QByteArray::number(e.expiresAt);
}

QByteArray CryptoManager::hkdfSha256(const QByteArray &secret, const QByteArray &salt,
                                     const QByteArray &info) {
    const QByteArray prk = hmacSha256(salt, secret);
    return hmacSha256(prk, info + QByteArray(1, '\x01'));
}

QByteArray CryptoManager::directionKey(const QByteArray &peerPublic,
                                       const QString &senderId,
                                       const QString &recipientId,
                                       QString *error) const {
    if (peerPublic.size() != crypto_scalarmult_curve25519_BYTES) {
        if (error) {
            *error = QStringLiteral("对端 X25519 公钥无效");
        }
        return {};
    }
    QByteArray shared(crypto_scalarmult_curve25519_BYTES, Qt::Uninitialized);
    if (crypto_scalarmult_curve25519(
            reinterpret_cast<unsigned char *>(shared.data()),
            reinterpret_cast<const unsigned char *>(xPrivate_.constData()),
            reinterpret_cast<const unsigned char *>(peerPublic.constData())) != 0) {
        if (error) {
            *error = QStringLiteral("X25519 密钥协商失败");
        }
        return {};
    }
    const QString first = std::min(senderId, recipientId);
    const QString second = std::max(senderId, recipientId);
    const QByteArray salt = QCryptographicHash::hash(
        QByteArrayLiteral("ClipBridge pairing v1") + first.toUtf8() +
            second.toUtf8(),
        QCryptographicHash::Sha256);
    const QByteArray info = QByteArrayLiteral("ClipBridge message v1") +
                            senderId.toUtf8() + recipientId.toUtf8();
    QByteArray key = hkdfSha256(shared, salt, info);
    sodium_memzero(shared.data(), static_cast<size_t>(shared.size()));
    return key;
}

Envelope CryptoManager::encryptText(const QString &text, const QString &senderId,
                                    const Device &recipient, qint64 ttlMs,
                                    bool offlineAllowed, QString *error) const {
    Envelope envelope;
    envelope.messageId = QUuid::createUuid().toString(QUuid::WithoutBraces);
    envelope.senderDeviceId = senderId;
    envelope.recipientDeviceId = recipient.id;
    envelope.createdAt = QDateTime::currentMSecsSinceEpoch();
    envelope.expiresAt = envelope.createdAt + ttlMs;
    envelope.offlineAllowed = offlineAllowed;
    envelope.nonce.resize(crypto_aead_xchacha20poly1305_ietf_NPUBBYTES);
    randombytes_buf(envelope.nonce.data(), static_cast<size_t>(envelope.nonce.size()));

    const QJsonObject payload{
        {QStringLiteral("text"), text},
        {QStringLiteral("content_sha256"),
         QString::fromLatin1(toBase64Url(contentHash(text)))},
        {QStringLiteral("sensitive"), false},
    };
    const QByteArray plain = QJsonDocument(payload).toJson(QJsonDocument::Compact);
    const QByteArray metadata = aad(envelope);
    QByteArray key = directionKey(recipient.x25519PublicKey, senderId, recipient.id, error);
    if (key.isEmpty()) {
        return {};
    }
    envelope.ciphertext.resize(
        plain.size() + crypto_aead_xchacha20poly1305_ietf_ABYTES);
    unsigned long long cipherLength = 0;
    crypto_aead_xchacha20poly1305_ietf_encrypt(
        reinterpret_cast<unsigned char *>(envelope.ciphertext.data()), &cipherLength,
        reinterpret_cast<const unsigned char *>(plain.constData()), plain.size(),
        reinterpret_cast<const unsigned char *>(metadata.constData()), metadata.size(),
        nullptr, reinterpret_cast<const unsigned char *>(envelope.nonce.constData()),
        reinterpret_cast<const unsigned char *>(key.constData()));
    envelope.ciphertext.resize(static_cast<qsizetype>(cipherLength));
    sodium_memzero(key.data(), static_cast<size_t>(key.size()));

    const QByteArray digest = QCryptographicHash::hash(
        metadata + envelope.nonce + envelope.ciphertext, QCryptographicHash::Sha256);
    envelope.signature.resize(crypto_sign_BYTES);
    crypto_sign_detached(
        reinterpret_cast<unsigned char *>(envelope.signature.data()), nullptr,
        reinterpret_cast<const unsigned char *>(digest.constData()), digest.size(),
        reinterpret_cast<const unsigned char *>(signPrivate_.constData()));
    return envelope;
}

QString CryptoManager::decryptText(const Envelope &envelope, const Device &sender,
                                   QString *error) const {
    if (sender.ed25519PublicKey.size() != crypto_sign_PUBLICKEYBYTES ||
        envelope.nonce.size() != crypto_aead_xchacha20poly1305_ietf_NPUBBYTES ||
        envelope.signature.size() != crypto_sign_BYTES) {
        if (error) {
            *error = QStringLiteral("消息密钥或签名长度无效");
        }
        return {};
    }
    const QByteArray metadata = aad(envelope);
    const QByteArray digest = QCryptographicHash::hash(
        metadata + envelope.nonce + envelope.ciphertext, QCryptographicHash::Sha256);
    if (crypto_sign_verify_detached(
            reinterpret_cast<const unsigned char *>(envelope.signature.constData()),
            reinterpret_cast<const unsigned char *>(digest.constData()), digest.size(),
            reinterpret_cast<const unsigned char *>(sender.ed25519PublicKey.constData())) != 0) {
        if (error) {
            *error = QStringLiteral("发送设备签名验证失败");
        }
        return {};
    }
    QByteArray key = directionKey(sender.x25519PublicKey, envelope.senderDeviceId,
                                  envelope.recipientDeviceId, error);
    if (key.isEmpty()) {
        return {};
    }
    if (envelope.ciphertext.size() < crypto_aead_xchacha20poly1305_ietf_ABYTES) {
        if (error) {
            *error = QStringLiteral("密文长度无效");
        }
        return {};
    }
    QByteArray plain(envelope.ciphertext.size() -
                         crypto_aead_xchacha20poly1305_ietf_ABYTES,
                     Qt::Uninitialized);
    unsigned long long plainLength = 0;
    const int result = crypto_aead_xchacha20poly1305_ietf_decrypt(
        reinterpret_cast<unsigned char *>(plain.data()), &plainLength, nullptr,
        reinterpret_cast<const unsigned char *>(envelope.ciphertext.constData()),
        envelope.ciphertext.size(),
        reinterpret_cast<const unsigned char *>(metadata.constData()), metadata.size(),
        reinterpret_cast<const unsigned char *>(envelope.nonce.constData()),
        reinterpret_cast<const unsigned char *>(key.constData()));
    sodium_memzero(key.data(), static_cast<size_t>(key.size()));
    if (result != 0) {
        if (error) {
            *error = QStringLiteral("密文认证失败");
        }
        return {};
    }
    plain.resize(static_cast<qsizetype>(plainLength));
    QJsonParseError parseError;
    const QJsonObject payload = QJsonDocument::fromJson(plain, &parseError).object();
    const QString text = payload.value(QStringLiteral("text")).toString();
    const QByteArray claimed =
        fromBase64Url(payload.value(QStringLiteral("content_sha256")).toString());
    if (parseError.error != QJsonParseError::NoError || claimed != contentHash(text)) {
        if (error) {
            *error = QStringLiteral("明文摘要验证失败");
        }
        return {};
    }
    return text;
}

