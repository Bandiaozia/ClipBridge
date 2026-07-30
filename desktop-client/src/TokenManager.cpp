#include "TokenManager.h"

#include <QJsonDocument>
#include <QJsonObject>

TokenManager::TokenManager() = default;

TokenPair TokenManager::tokens() const {
    const QByteArray raw = storage_.load(QStringLiteral("tokens-v1"));
    const QJsonObject object = QJsonDocument::fromJson(raw).object();
    return {
        object.value(QStringLiteral("access_token")).toString(),
        object.value(QStringLiteral("refresh_token")).toString(),
        object.value(QStringLiteral("expires_at")).toInteger(),
    };
}

bool TokenManager::setTokens(const TokenPair &tokens, QString *error) {
    const QJsonObject object{
        {QStringLiteral("access_token"), tokens.accessToken},
        {QStringLiteral("refresh_token"), tokens.refreshToken},
        {QStringLiteral("expires_at"), tokens.expiresAt},
    };
    return storage_.save(QStringLiteral("tokens-v1"),
                         QJsonDocument(object).toJson(QJsonDocument::Compact), error);
}

bool TokenManager::clear(QString *error) {
    return storage_.remove(QStringLiteral("tokens-v1"), error);
}

bool TokenManager::needsRefresh(qint64 now) const {
    const TokenPair pair = tokens();
    return pair.isValid() && pair.expiresAt <= now + 60000;
}

