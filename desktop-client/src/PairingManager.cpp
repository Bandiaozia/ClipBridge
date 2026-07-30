#include "PairingManager.h"

#include "ApiClient.h"
#include "CryptoManager.h"
#include "DeviceManager.h"
#include "SettingsRepository.h"

#include <QJsonDocument>
#include <QJsonObject>

#include <qrencode.h>

PairingManager::PairingManager(ApiClient *api, SettingsRepository *settings,
                               DeviceManager *devices, CryptoManager *crypto,
                               QObject *parent)
    : QObject(parent),
      api_(api),
      settings_(settings),
      devices_(devices),
      crypto_(crypto) {
    connect(api_, &ApiClient::pairingCreated, this,
            [this](const QJsonObject &response) {
                const QString token =
                    response.value(QStringLiteral("token")).toString();
                const qint64 expires =
                    response.value(QStringLiteral("expires_at")).toInteger();
                if (token.isEmpty() || expires <= 0) {
                    emit failed(QStringLiteral("服务器返回的配对信息无效"));
                    return;
                }
                const QJsonObject payload{
                    {QStringLiteral("version"), 1},
                    {QStringLiteral("server"), settings_->serverUrl().toString()},
                    {QStringLiteral("token"), token},
                    {QStringLiteral("initiator_device_id"), devices_->deviceId()},
                    {QStringLiteral("x25519_public_key"),
                     QString::fromLatin1(CryptoManager::toBase64Url(
                         crypto_->x25519PublicKey()))},
                    {QStringLiteral("ed25519_public_key"),
                     QString::fromLatin1(CryptoManager::toBase64Url(
                         crypto_->ed25519PublicKey()))},
                    {QStringLiteral("expires_at"), expires},
                };
                const QByteArray compact =
                    QJsonDocument(payload).toJson(QJsonDocument::Compact);
                const QImage image = renderQr(compact);
                if (image.isNull()) {
                    emit failed(QStringLiteral("二维码生成失败"));
                    return;
                }
                emit qrReady(image, QString::fromUtf8(compact), expires);
            });
    connect(api_, &ApiClient::requestFailed, this, &PairingManager::failed);
}

void PairingManager::create(const QString &accessToken) {
    if (accessToken.isEmpty()) {
        emit failed(QStringLiteral("请先登录"));
        return;
    }
    api_->createPairing(accessToken);
}

QImage PairingManager::renderQr(const QByteArray &payload) {
    QRcode *code = QRcode_encodeString(
        payload.constData(), 0, QR_ECLEVEL_M, QR_MODE_8, 1);
    if (!code) return {};
    constexpr int scale = 6;
    constexpr int quiet = 4;
    const int modules = code->width + quiet * 2;
    QImage image(modules * scale, modules * scale, QImage::Format_RGB32);
    image.fill(Qt::white);
    for (int y = 0; y < code->width; ++y) {
        for (int x = 0; x < code->width; ++x) {
            if ((code->data[y * code->width + x] & 1U) == 0U) continue;
            for (int dy = 0; dy < scale; ++dy) {
                for (int dx = 0; dx < scale; ++dx) {
                    image.setPixel((x + quiet) * scale + dx,
                                   (y + quiet) * scale + dy, qRgb(0, 0, 0));
                }
            }
        }
    }
    QRcode_free(code);
    return image;
}

