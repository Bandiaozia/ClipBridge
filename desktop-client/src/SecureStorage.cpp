#include "SecureStorage.h"

#include <QCryptographicHash>
#include <QDir>
#include <QFile>
#include <QSaveFile>
#include <QStandardPaths>

#include <sodium.h>

#ifdef Q_OS_WIN
#include <windows.h>
#include <wincred.h>
#endif

namespace {
constexpr auto kCredentialPrefix = "ClipBridge/";
}

SecureStorage::SecureStorage() {
    if (sodium_init() < 0) {
        qFatal("libsodium 初始化失败");
    }
}

#ifdef Q_OS_WIN
QByteArray SecureStorage::load(const QString &key, QString *error) const {
    PCREDENTIALW credential = nullptr;
    const QString target = QString::fromLatin1(kCredentialPrefix) + key;
    if (!CredReadW(reinterpret_cast<LPCWSTR>(target.utf16()), CRED_TYPE_GENERIC, 0,
                   &credential)) {
        if (GetLastError() != ERROR_NOT_FOUND && error) {
            *error = QStringLiteral("读取 Windows Credential Manager 失败");
        }
        return {};
    }
    const QByteArray result(
        reinterpret_cast<const char *>(credential->CredentialBlob),
        static_cast<qsizetype>(credential->CredentialBlobSize));
    CredFree(credential);
    return result;
}

bool SecureStorage::save(const QString &key, const QByteArray &value, QString *error) const {
    const QString target = QString::fromLatin1(kCredentialPrefix) + key;
    CREDENTIALW credential{};
    credential.Type = CRED_TYPE_GENERIC;
    credential.TargetName = const_cast<LPWSTR>(
        reinterpret_cast<LPCWSTR>(target.utf16()));
    credential.CredentialBlobSize = static_cast<DWORD>(value.size());
    credential.CredentialBlob = reinterpret_cast<LPBYTE>(
        const_cast<char *>(value.constData()));
    credential.Persist = CRED_PERSIST_LOCAL_MACHINE;
    if (!CredWriteW(&credential, 0)) {
        if (error) {
            *error = QStringLiteral("写入 Windows Credential Manager 失败");
        }
        return false;
    }
    return true;
}

bool SecureStorage::remove(const QString &key, QString *error) const {
    const QString target = QString::fromLatin1(kCredentialPrefix) + key;
    if (!CredDeleteW(reinterpret_cast<LPCWSTR>(target.utf16()), CRED_TYPE_GENERIC, 0) &&
        GetLastError() != ERROR_NOT_FOUND) {
        if (error) {
            *error = QStringLiteral("删除 Windows 凭据失败");
        }
        return false;
    }
    return true;
}
#else
QString SecureStorage::pathForKey(const QString &key) const {
    const QByteArray digest =
        QCryptographicHash::hash(key.toUtf8(), QCryptographicHash::Sha256).toHex();
    return QStandardPaths::writableLocation(QStandardPaths::AppConfigLocation) +
           QStringLiteral("/secure-") + QString::fromLatin1(digest) +
           QStringLiteral(".bin");
}

QByteArray SecureStorage::wrappingKey() const {
    QFile machineId(QStringLiteral("/etc/machine-id"));
    QByteArray material;
    if (machineId.open(QIODevice::ReadOnly)) {
        material = machineId.readAll().trimmed();
    }
    material += '\0';
    material += qgetenv("USER");
    material += QByteArrayLiteral("\0ClipBridge local credential wrapping v1");
    QByteArray key(crypto_secretbox_KEYBYTES, Qt::Uninitialized);
    crypto_generichash(reinterpret_cast<unsigned char *>(key.data()), key.size(),
                       reinterpret_cast<const unsigned char *>(material.constData()),
                       static_cast<unsigned long long>(material.size()), nullptr, 0);
    sodium_memzero(material.data(), static_cast<size_t>(material.size()));
    return key;
}

QByteArray SecureStorage::load(const QString &key, QString *error) const {
    QFile file(pathForKey(key));
    if (!file.exists()) {
        return {};
    }
    if (!file.open(QIODevice::ReadOnly)) {
        if (error) {
            *error = QStringLiteral("无法读取安全配置文件");
        }
        return {};
    }
    const QByteArray encoded = file.readAll();
    const qsizetype minimum = 4 + crypto_secretbox_NONCEBYTES + crypto_secretbox_MACBYTES;
    if (encoded.size() < minimum || encoded.left(4) != QByteArrayLiteral("CB01")) {
        if (error) {
            *error = QStringLiteral("安全配置文件格式无效");
        }
        return {};
    }
    const QByteArray nonce = encoded.mid(4, crypto_secretbox_NONCEBYTES);
    const QByteArray cipher = encoded.mid(4 + crypto_secretbox_NONCEBYTES);
    QByteArray plain(cipher.size() - crypto_secretbox_MACBYTES, Qt::Uninitialized);
    QByteArray wrap = wrappingKey();
    const int result = crypto_secretbox_open_easy(
        reinterpret_cast<unsigned char *>(plain.data()),
        reinterpret_cast<const unsigned char *>(cipher.constData()),
        static_cast<unsigned long long>(cipher.size()),
        reinterpret_cast<const unsigned char *>(nonce.constData()),
        reinterpret_cast<const unsigned char *>(wrap.constData()));
    sodium_memzero(wrap.data(), static_cast<size_t>(wrap.size()));
    if (result != 0) {
        if (error) {
            *error = QStringLiteral("安全配置文件认证失败");
        }
        sodium_memzero(plain.data(), static_cast<size_t>(plain.size()));
        return {};
    }
    return plain;
}

bool SecureStorage::save(const QString &key, const QByteArray &value, QString *error) const {
    const QString path = pathForKey(key);
    if (!QDir().mkpath(QFileInfo(path).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建安全配置目录");
        }
        return false;
    }
    QByteArray nonce(crypto_secretbox_NONCEBYTES, Qt::Uninitialized);
    randombytes_buf(nonce.data(), static_cast<size_t>(nonce.size()));
    QByteArray cipher(value.size() + crypto_secretbox_MACBYTES, Qt::Uninitialized);
    QByteArray wrap = wrappingKey();
    crypto_secretbox_easy(
        reinterpret_cast<unsigned char *>(cipher.data()),
        reinterpret_cast<const unsigned char *>(value.constData()),
        static_cast<unsigned long long>(value.size()),
        reinterpret_cast<const unsigned char *>(nonce.constData()),
        reinterpret_cast<const unsigned char *>(wrap.constData()));
    sodium_memzero(wrap.data(), static_cast<size_t>(wrap.size()));

    QSaveFile file(path);
    if (!file.open(QIODevice::WriteOnly)) {
        if (error) {
            *error = QStringLiteral("无法写入安全配置文件");
        }
        return false;
    }
    file.setPermissions(QFileDevice::ReadOwner | QFileDevice::WriteOwner);
    if (file.write(QByteArrayLiteral("CB01") + nonce + cipher) < 0 || !file.commit()) {
        if (error) {
            *error = QStringLiteral("提交安全配置文件失败");
        }
        return false;
    }
    QFile::setPermissions(path, QFileDevice::ReadOwner | QFileDevice::WriteOwner);
    return true;
}

bool SecureStorage::remove(const QString &key, QString *error) const {
    const QString path = pathForKey(key);
    if (QFile::exists(path) && !QFile::remove(path)) {
        if (error) {
            *error = QStringLiteral("删除安全配置文件失败");
        }
        return false;
    }
    return true;
}
#endif

