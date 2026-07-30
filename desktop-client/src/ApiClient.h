#pragma once

#include "Models.h"

#include <QJsonObject>
#include <QNetworkAccessManager>
#include <QObject>

class CryptoManager;
class DeviceManager;
class SettingsRepository;

class ApiClient final : public QObject {
    Q_OBJECT

public:
    ApiClient(SettingsRepository *settings, DeviceManager *devices,
              CryptoManager *crypto, QObject *parent = nullptr);

    void authenticate(const QString &email, const QString &password, bool registration);
    void refresh(const QString &refreshToken);
    void fetchDevices(const QString &accessToken);
    void logout(const QString &accessToken, const QString &refreshToken);
    void createPairing(const QString &accessToken);

signals:
    void authenticated(const TokenPair &tokens);
    void refreshed(const TokenPair &tokens);
    void devicesLoaded(const QVector<Device> &devices);
    void loggedOut();
    void pairingCreated(const QJsonObject &pairing);
    void requestFailed(const QString &message);

private:
    QUrl endpoint(const QString &path) const;
    QNetworkReply *sendJson(const QString &method, const QString &path,
                            const QJsonObject &body, const QString &accessToken = {});
    static TokenPair parseTokens(const QJsonObject &object);
    static QString errorMessage(QNetworkReply *reply, const QByteArray &body);

    SettingsRepository *settings_;
    DeviceManager *devices_;
    CryptoManager *crypto_;
    QNetworkAccessManager network_;
};
