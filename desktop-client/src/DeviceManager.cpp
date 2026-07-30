#include "DeviceManager.h"

#include <QSysInfo>
#include <QUuid>

DeviceManager::DeviceManager(QObject *parent)
    : QObject(parent),
      settings_(QStringLiteral("ClipBridge"), QStringLiteral("ClipBridge")) {
    deviceId_ = settings_.value(QStringLiteral("device/id")).toString();
    if (QUuid(deviceId_).isNull()) {
        deviceId_ = QUuid::createUuid().toString(QUuid::WithoutBraces);
        settings_.setValue(QStringLiteral("device/id"), deviceId_);
    }
    deviceName_ = settings_.value(QStringLiteral("device/name")).toString();
    if (deviceName_.trimmed().isEmpty()) {
        deviceName_ = QSysInfo::machineHostName().trimmed();
        if (deviceName_.isEmpty()) deviceName_ = QStringLiteral("ClipBridge Desktop");
        settings_.setValue(QStringLiteral("device/name"), deviceName_);
    }
}

QString DeviceManager::deviceId() const { return deviceId_; }
QString DeviceManager::deviceName() const { return deviceName_; }

QString DeviceManager::platform() const {
#ifdef Q_OS_WIN
    return QStringLiteral("windows");
#else
    return QStringLiteral("linux");
#endif
}

QVector<Device> DeviceManager::devices() const { return devices_; }

Device DeviceManager::device(const QString &id) const {
    for (const Device &device : devices_) {
        if (device.id == id) return device;
    }
    return {};
}

void DeviceManager::replaceDevices(const QVector<Device> &devices) {
    devices_ = devices;
    emit devicesChanged();
}

void DeviceManager::setOnline(const QString &id, bool online) {
    for (Device &device : devices_) {
        if (device.id == id && device.online != online) {
            device.online = online;
            emit devicesChanged();
            return;
        }
    }
}

void DeviceManager::revoke(const QString &id) {
    for (Device &device : devices_) {
        if (device.id == id) {
            device.revoked = true;
            device.online = false;
            emit devicesChanged();
            return;
        }
    }
}

