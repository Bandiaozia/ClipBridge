#pragma once

#include <QString>
#include <QStringList>

class SensitiveContentDetector final {
public:
    [[nodiscard]] QStringList detect(const QString &text) const;
    [[nodiscard]] bool isSensitive(const QString &text) const;
};

