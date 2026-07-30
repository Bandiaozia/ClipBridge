#include "AutoStartManager.h"

#include <QCoreApplication>
#include <QDir>
#include <QFile>
#include <QSaveFile>
#include <QSettings>
#include <QStandardPaths>

bool AutoStartManager::isEnabled() const {
#ifdef Q_OS_WIN
    QSettings run(QStringLiteral(
                      "HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\Run"),
                  QSettings::NativeFormat);
    return run.contains(QStringLiteral("ClipBridge"));
#else
    return QFile::exists(QStandardPaths::writableLocation(QStandardPaths::ConfigLocation) +
                         QStringLiteral("/autostart/clipbridge.desktop"));
#endif
}

bool AutoStartManager::setEnabled(bool enabled) const {
#ifdef Q_OS_WIN
    QSettings run(QStringLiteral(
                      "HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\Run"),
                  QSettings::NativeFormat);
    if (enabled) {
        run.setValue(QStringLiteral("ClipBridge"),
                     QStringLiteral("\"%1\"").arg(QCoreApplication::applicationFilePath()));
    } else {
        run.remove(QStringLiteral("ClipBridge"));
    }
    return run.status() == QSettings::NoError;
#else
    const QString directory =
        QStandardPaths::writableLocation(QStandardPaths::ConfigLocation) +
        QStringLiteral("/autostart");
    const QString path = directory + QStringLiteral("/clipbridge.desktop");
    if (!enabled) return !QFile::exists(path) || QFile::remove(path);
    if (!QDir().mkpath(directory)) return false;
    QSaveFile file(path);
    if (!file.open(QIODevice::WriteOnly)) return false;
    const QByteArray desktop =
        QByteArrayLiteral("[Desktop Entry]\nType=Application\nName=ClipBridge\nExec=\"") +
        QCoreApplication::applicationFilePath().toUtf8() +
        QByteArrayLiteral("\"\nTerminal=false\nX-GNOME-Autostart-enabled=true\n");
    return file.write(desktop) == desktop.size() && file.commit();
#endif
}

