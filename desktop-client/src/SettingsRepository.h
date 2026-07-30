#pragma once

#include <QSettings>
#include <QStringList>
#include <QUrl>

class SettingsRepository final {
public:
    SettingsRepository();

    [[nodiscard]] QUrl serverUrl() const;
    void setServerUrl(const QUrl &url);
    [[nodiscard]] bool autoConnect() const;
    void setAutoConnect(bool enabled);
    [[nodiscard]] bool syncEnabled() const;
    void setSyncEnabled(bool enabled);
    [[nodiscard]] bool autoWriteRemote() const;
    void setAutoWriteRemote(bool enabled);
    [[nodiscard]] int maxTextLength() const;
    void setMaxTextLength(int length);
    [[nodiscard]] int retentionDays() const;
    void setRetentionDays(int days);
    [[nodiscard]] int maxHistoryItems() const;
    void setMaxHistoryItems(int items);
    [[nodiscard]] int offlineTtlSeconds() const;
    void setOfflineTtlSeconds(int seconds);
    [[nodiscard]] QString sensitivePolicy() const;
    void setSensitivePolicy(const QString &policy);
    [[nodiscard]] qint64 pausedUntil() const;
    void setPausedUntil(qint64 timestamp);
    [[nodiscard]] QStringList targetDeviceIds() const;
    void setTargetDeviceIds(const QStringList &ids);
    [[nodiscard]] QString themeColor() const;
    void setThemeColor(const QString &color);

private:
    mutable QSettings settings_;
};
