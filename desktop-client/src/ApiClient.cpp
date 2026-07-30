#include "ApiClient.h"

#include "CryptoManager.h"
#include "DeviceManager.h"
#include "SettingsRepository.h"

#include <QDateTime>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkReply>
#include <QTimer>

ApiClient::ApiClient(SettingsRepository *settings, DeviceManager *devices,
                     CryptoManager *crypto, QObject *parent)
    : QObject(parent), settings_(settings), devices_(devices), crypto_(crypto) {}

QUrl ApiClient::endpoint(const QString &path) const {
    QUrl url = settings_->serverUrl();
    url.setPath(path);
    url.setQuery(QString());
    return url;
}

QNetworkReply *ApiClient::sendJson(const QString &method, const QString &path,
                                   const QJsonObject &body,
                                   const QString &accessToken) {
    QNetworkRequest request(endpoint(path));
    request.setHeader(QNetworkRequest::ContentTypeHeader,
                      QStringLiteral("application/json"));
    request.setTransferTimeout(15000);
    request.setRawHeader("User-Agent", "ClipBridge-Desktop/0.3");
    if (!accessToken.isEmpty()) {
        request.setRawHeader("Authorization",
                             QByteArrayLiteral("Bearer ") + accessToken.toUtf8());
    }
    const QByteArray data = QJsonDocument(body).toJson(QJsonDocument::Compact);
    if (method == QStringLiteral("POST")) return network_.post(request, data);
    if (method == QStringLiteral("DELETE")) {
        return network_.sendCustomRequest(request, QByteArrayLiteral("DELETE"), data);
    }
    return network_.get(request);
}

void ApiClient::authenticate(const QString &email, const QString &password,
                             bool registration) {
    const QJsonObject device{
        {QStringLiteral("id"), devices_->deviceId()},
        {QStringLiteral("name"), devices_->deviceName()},
        {QStringLiteral("platform"), devices_->platform()},
        {QStringLiteral("x25519_public_key"),
         QString::fromLatin1(CryptoManager::toBase64Url(crypto_->x25519PublicKey()))},
        {QStringLiteral("ed25519_public_key"),
         QString::fromLatin1(CryptoManager::toBase64Url(crypto_->ed25519PublicKey()))},
    };
    const QJsonObject request{
        {QStringLiteral("email"), email.trimmed()},
        {QStringLiteral("password"), password},
        {QStringLiteral("device"), device},
    };
    const QString path = registration ? QStringLiteral("/api/v1/auth/register")
                                      : QStringLiteral("/api/v1/auth/login");
    QNetworkReply *reply = sendJson(QStringLiteral("POST"), path, request);
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        const QByteArray body = reply->readAll();
        if (reply->error() != QNetworkReply::NoError) {
            emit requestFailed(errorMessage(reply, body));
        } else {
            const QJsonObject root = QJsonDocument::fromJson(body).object();
            emit authenticated(parseTokens(root.value(QStringLiteral("tokens")).toObject()));
        }
        reply->deleteLater();
    });
}

void ApiClient::refresh(const QString &refreshToken) {
    QNetworkReply *reply = sendJson(
        QStringLiteral("POST"), QStringLiteral("/api/v1/auth/refresh"),
        {{QStringLiteral("refresh_token"), refreshToken}});
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        const QByteArray body = reply->readAll();
        if (reply->error() != QNetworkReply::NoError) {
            emit requestFailed(errorMessage(reply, body));
        } else {
            emit refreshed(parseTokens(QJsonDocument::fromJson(body).object()));
        }
        reply->deleteLater();
    });
}

void ApiClient::fetchDevices(const QString &accessToken) {
    QNetworkReply *reply = sendJson(
        QStringLiteral("GET"), QStringLiteral("/api/v1/devices"), {}, accessToken);
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        const QByteArray body = reply->readAll();
        if (reply->error() != QNetworkReply::NoError) {
            emit requestFailed(errorMessage(reply, body));
        } else {
            QVector<Device> devices;
            const QJsonArray array = QJsonDocument::fromJson(body)
                                         .object()
                                         .value(QStringLiteral("devices"))
                                         .toArray();
            devices.reserve(array.size());
            for (const QJsonValue &value : array) {
                const QJsonObject item = value.toObject();
                Device device;
                device.id = item.value(QStringLiteral("id")).toString();
                device.name = item.value(QStringLiteral("name")).toString();
                device.platform = item.value(QStringLiteral("platform")).toString();
                device.x25519PublicKey = CryptoManager::fromBase64Url(
                    item.value(QStringLiteral("x25519_public_key")).toString());
                device.ed25519PublicKey = CryptoManager::fromBase64Url(
                    item.value(QStringLiteral("ed25519_public_key")).toString());
                device.online = item.value(QStringLiteral("online")).toBool();
                device.revoked = !item.value(QStringLiteral("revoked_at")).isUndefined();
                devices.push_back(device);
            }
            emit devicesLoaded(devices);
        }
        reply->deleteLater();
    });
}

void ApiClient::logout(const QString &accessToken, const QString &refreshToken) {
    QNetworkReply *reply = sendJson(
        QStringLiteral("POST"), QStringLiteral("/api/v1/auth/logout"),
        {{QStringLiteral("refresh_token"), refreshToken}}, accessToken);
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        if (reply->error() != QNetworkReply::NoError &&
            reply->attribute(QNetworkRequest::HttpStatusCodeAttribute).toInt() != 401) {
            emit requestFailed(errorMessage(reply, reply->readAll()));
        } else {
            emit loggedOut();
        }
        reply->deleteLater();
    });
}

void ApiClient::createPairing(const QString &accessToken) {
    QNetworkReply *reply = sendJson(
        QStringLiteral("POST"), QStringLiteral("/api/v1/pairing/create"), {},
        accessToken);
    connect(reply, &QNetworkReply::finished, this, [this, reply] {
        const QByteArray body = reply->readAll();
        if (reply->error() != QNetworkReply::NoError) {
            emit requestFailed(errorMessage(reply, body));
        } else {
            emit pairingCreated(QJsonDocument::fromJson(body).object());
        }
        reply->deleteLater();
    });
}

TokenPair ApiClient::parseTokens(const QJsonObject &object) {
    TokenPair pair;
    pair.accessToken = object.value(QStringLiteral("access_token")).toString();
    pair.refreshToken = object.value(QStringLiteral("refresh_token")).toString();
    pair.expiresAt = QDateTime::currentMSecsSinceEpoch() +
                     object.value(QStringLiteral("expires_in")).toInteger() * 1000;
    return pair;
}

QString ApiClient::errorMessage(QNetworkReply *reply, const QByteArray &body) {
    const QJsonObject root = QJsonDocument::fromJson(body).object();
    const QJsonObject error = root.value(QStringLiteral("error")).toObject();
    const QString message = error.value(QStringLiteral("message")).toString();
    return message.isEmpty() ? reply->errorString() : message;
}
