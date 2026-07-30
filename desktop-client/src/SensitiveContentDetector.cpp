#include "SensitiveContentDetector.h"

#include <QRegularExpression>

QStringList SensitiveContentDetector::detect(const QString &text) const {
    struct Rule {
        const char *name;
        const char *pattern;
    };
    static const Rule rules[] = {
        {"六位验证码", R"((?:^|\D)\d{6}(?:\D|$))"},
        {"疑似银行卡号", R"((?:\d[ -]?){13,19})"},
        {"疑似身份证号", R"((?:^|\D)\d{17}[\dXx](?:\D|$))"},
        {"私钥", R"(-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----)"},
        {"Bearer Token", R"((?i)\bBearer\s+[A-Za-z0-9._~+/\-=]{16,})"},
        {"JWT", R"(\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b)"},
        {"API Key", R"((?i)\b(?:api[_-]?key|secret[_-]?key)\s*[:=]\s*\S{12,})"},
        {"数据库连接串", R"((?i)\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis)://\S+)"},
        {"疑似密码", R"((?i)\b(?:password|passwd|pwd)\s*[:=]\s*\S{6,})"},
    };
    QStringList matches;
    for (const Rule &rule : rules) {
        const QRegularExpression regex(QString::fromLatin1(rule.pattern));
        if (regex.match(text).hasMatch()) {
            matches.push_back(QString::fromUtf8(rule.name));
        }
    }
    return matches;
}

bool SensitiveContentDetector::isSensitive(const QString &text) const {
    return !detect(text).isEmpty();
}

