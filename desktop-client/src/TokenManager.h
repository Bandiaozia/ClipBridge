#pragma once

#include "Models.h"
#include "SecureStorage.h"

class TokenManager final {
public:
    TokenManager();

    [[nodiscard]] TokenPair tokens() const;
    bool setTokens(const TokenPair &tokens, QString *error = nullptr);
    bool clear(QString *error = nullptr);
    [[nodiscard]] bool needsRefresh(qint64 now) const;

private:
    SecureStorage storage_;
};

