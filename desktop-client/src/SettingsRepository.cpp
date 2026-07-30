#include "SettingsRepository.h"

SettingsRepository::SettingsRepository()
    : settings_(QStringLiteral("ClipBridge"), QStringLiteral("ClipBridge")) {}

QUrl SettingsRepository::serverUrl() const {
    return QUrl(settings_.value(QStringLiteral("server/url"),
                                QStringLiteral("https://clipbridge.ccttkx.xyz"))
                    .toString());
}

void SettingsRepository::setServerUrl(const QUrl &url) {
    settings_.setValue(QStringLiteral("server/url"), url.toString());
    settings_.sync();
}

bool SettingsRepository::autoConnect() const {
    return settings_.value(QStringLiteral("connection/auto"), true).toBool();
}

void SettingsRepository::setAutoConnect(bool enabled) {
    settings_.setValue(QStringLiteral("connection/auto"), enabled);
}

bool SettingsRepository::syncEnabled() const {
    return settings_.value(QStringLiteral("sync/enabled"), true).toBool();
}

void SettingsRepository::setSyncEnabled(bool enabled) {
    settings_.setValue(QStringLiteral("sync/enabled"), enabled);
}

bool SettingsRepository::autoWriteRemote() const {
    return settings_.value(QStringLiteral("sync/autoWriteRemote"), true).toBool();
}

void SettingsRepository::setAutoWriteRemote(bool enabled) {
    settings_.setValue(QStringLiteral("sync/autoWriteRemote"), enabled);
}

int SettingsRepository::maxTextLength() const {
    return settings_.value(QStringLiteral("sync/maxTextLength"), 100000).toInt();
}

void SettingsRepository::setMaxTextLength(int length) {
    settings_.setValue(QStringLiteral("sync/maxTextLength"), length);
}

int SettingsRepository::retentionDays() const {
    return settings_.value(QStringLiteral("history/retentionDays"), 30).toInt();
}

void SettingsRepository::setRetentionDays(int days) {
    settings_.setValue(QStringLiteral("history/retentionDays"), days);
}

int SettingsRepository::maxHistoryItems() const {
    return settings_.value(QStringLiteral("history/maxItems"), 2000).toInt();
}

void SettingsRepository::setMaxHistoryItems(int items) {
    settings_.setValue(QStringLiteral("history/maxItems"), items);
}

int SettingsRepository::offlineTtlSeconds() const {
    return settings_.value(QStringLiteral("sync/offlineTtl"), 600).toInt();
}

void SettingsRepository::setOfflineTtlSeconds(int seconds) {
    settings_.setValue(QStringLiteral("sync/offlineTtl"), seconds);
}

QString SettingsRepository::sensitivePolicy() const {
    return settings_.value(QStringLiteral("sensitive/policy"),
                           QStringLiteral("confirm"))
        .toString();
}

void SettingsRepository::setSensitivePolicy(const QString &policy) {
    settings_.setValue(QStringLiteral("sensitive/policy"), policy);
}

qint64 SettingsRepository::pausedUntil() const {
    return settings_.value(QStringLiteral("sync/pausedUntil"), 0).toLongLong();
}

void SettingsRepository::setPausedUntil(qint64 timestamp) {
    settings_.setValue(QStringLiteral("sync/pausedUntil"), timestamp);
}

QStringList SettingsRepository::targetDeviceIds() const {
    return settings_.value(QStringLiteral("sync/targetDeviceIds")).toStringList();
}

void SettingsRepository::setTargetDeviceIds(const QStringList &ids) {
    settings_.setValue(QStringLiteral("sync/targetDeviceIds"), ids);
    settings_.sync();
}

QString SettingsRepository::themeColor() const {
    return settings_.value(QStringLiteral("appearance/themeColor"),
                           QStringLiteral("blue"))
        .toString();
}

void SettingsRepository::setThemeColor(const QString &color) {
    static const QStringList allowed{
        QStringLiteral("blue"), QStringLiteral("green"),
        QStringLiteral("purple"), QStringLiteral("orange"),
        QStringLiteral("neutral"),
    };
    if (!allowed.contains(color)) return;
    settings_.setValue(QStringLiteral("appearance/themeColor"), color);
    settings_.sync();
}
