#pragma once

#include <QImage>
#include <QObject>

class ApiClient;
class CryptoManager;
class DeviceManager;
class SettingsRepository;

class PairingManager final : public QObject {
    Q_OBJECT

public:
    PairingManager(ApiClient *api, SettingsRepository *settings,
                   DeviceManager *devices, CryptoManager *crypto,
                   QObject *parent = nullptr);
    void create(const QString &accessToken);

signals:
    void qrReady(const QImage &image, const QString &payload, qint64 expiresAt);
    void failed(const QString &message);

private:
    static QImage renderQr(const QByteArray &payload);

    ApiClient *api_;
    SettingsRepository *settings_;
    DeviceManager *devices_;
    CryptoManager *crypto_;
};

