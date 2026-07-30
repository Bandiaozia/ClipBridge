#pragma once

#include "Models.h"

#include <QSqlDatabase>

class HistoryRepository final {
public:
    HistoryRepository();
    ~HistoryRepository();

    bool open(QString *error = nullptr);
    void close();
    bool add(const HistoryRecord &record, QString *error = nullptr);
    [[nodiscard]] bool containsMessage(const QString &messageId) const;
    [[nodiscard]] QVector<HistoryRecord> search(const QString &query = {},
                                                const QString &deviceId = {},
                                                int limit = 500) const;
    bool setFavorite(qint64 id, bool favorite);
    bool markSent(const QString &messageId);
    bool remove(qint64 id);
    bool clear();
    bool cleanup(int retentionDays, int maxItems);

private:
    QString connectionName_;
    QSqlDatabase database_;
};

