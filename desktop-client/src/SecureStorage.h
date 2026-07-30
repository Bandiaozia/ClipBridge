#pragma once

#include <QByteArray>
#include <QString>

class SecureStorage final {
public:
    SecureStorage();

    [[nodiscard]] QByteArray load(const QString &key, QString *error = nullptr) const;
    bool save(const QString &key, const QByteArray &value, QString *error = nullptr) const;
    bool remove(const QString &key, QString *error = nullptr) const;

private:
#ifndef Q_OS_WIN
    [[nodiscard]] QString pathForKey(const QString &key) const;
    [[nodiscard]] QByteArray wrappingKey() const;
#endif
};

