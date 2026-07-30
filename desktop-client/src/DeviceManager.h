#pragma once

#include "Models.h"

#include <QObject>
#include <QSettings>

class DeviceManager final : public QObject {
    Q_OBJECT

public:
    explicit DeviceManager(QObject *parent = nullptr);

    [[nodiscard]] QString deviceId() const;
    [[nodiscard]] QString deviceName() const;
    [[nodiscard]] QString platform() const;
    [[nodiscard]] QVector<Device> devices() const;
    [[nodiscard]] Device device(const QString &id) const;
    void replaceDevices(const QVector<Device> &devices);
    void setOnline(const QString &id, bool online);
    void revoke(const QString &id);

signals:
    void devicesChanged();

private:
    QSettings settings_;
    QString deviceId_;
    QString deviceName_;
    QVector<Device> devices_;
};

