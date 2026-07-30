#include "HistoryRepository.h"

#include <QDateTime>
#include <QDir>
#include <QSqlError>
#include <QSqlQuery>
#include <QStandardPaths>
#include <QUuid>

HistoryRepository::HistoryRepository()
    : connectionName_(QStringLiteral("clipbridge-history-") +
                      QUuid::createUuid().toString(QUuid::WithoutBraces)) {}

HistoryRepository::~HistoryRepository() { close(); }

bool HistoryRepository::open(QString *error) {
    const QString directory =
        QStandardPaths::writableLocation(QStandardPaths::AppLocalDataLocation);
    if (!QDir().mkpath(directory)) {
        if (error) *error = QStringLiteral("无法创建历史数据库目录");
        return false;
    }
    database_ = QSqlDatabase::addDatabase(QStringLiteral("QSQLITE"), connectionName_);
    database_.setDatabaseName(directory + QStringLiteral("/history.db"));
    if (!database_.open()) {
        if (error) *error = database_.lastError().text();
        return false;
    }
    QSqlQuery query(database_);
    if (!query.exec(QStringLiteral("PRAGMA journal_mode=WAL")) ||
        !query.exec(QStringLiteral("PRAGMA foreign_keys=ON")) ||
        !query.exec(QStringLiteral(
            "CREATE TABLE IF NOT EXISTS clipboard_history("
            "id INTEGER PRIMARY KEY AUTOINCREMENT,"
            "message_id TEXT NOT NULL UNIQUE,"
            "content TEXT NOT NULL,"
            "content_hash BLOB NOT NULL,"
            "source_device TEXT NOT NULL,"
            "target_device TEXT NOT NULL,"
            "created_at INTEGER NOT NULL,"
            "received_at INTEGER NOT NULL,"
            "favorite INTEGER NOT NULL DEFAULT 0,"
            "is_read INTEGER NOT NULL DEFAULT 0,"
            "sent INTEGER NOT NULL DEFAULT 0,"
            "local_created INTEGER NOT NULL DEFAULT 0,"
            "expired INTEGER NOT NULL DEFAULT 0)")) ||
        !query.exec(QStringLiteral(
            "CREATE INDEX IF NOT EXISTS idx_history_created "
            "ON clipboard_history(created_at DESC)")) ||
        !query.exec(QStringLiteral(
            "CREATE INDEX IF NOT EXISTS idx_history_source "
            "ON clipboard_history(source_device,created_at DESC)"))) {
        if (error) *error = query.lastError().text();
        return false;
    }
    return true;
}

void HistoryRepository::close() {
    if (database_.isValid()) {
        database_.close();
        database_ = {};
        QSqlDatabase::removeDatabase(connectionName_);
    }
}

bool HistoryRepository::add(const HistoryRecord &record, QString *error) {
    QSqlQuery query(database_);
    query.prepare(QStringLiteral(
        "INSERT OR IGNORE INTO clipboard_history("
        "message_id,content,content_hash,source_device,target_device,created_at,"
        "received_at,favorite,is_read,sent,local_created,expired)"
        "VALUES(?,?,?,?,?,?,?,?,?,?,?,?)"));
    query.addBindValue(record.messageId);
    query.addBindValue(record.content);
    query.addBindValue(record.contentHash);
    query.addBindValue(record.sourceDevice);
    query.addBindValue(record.targetDevice);
    query.addBindValue(record.createdAt);
    query.addBindValue(record.receivedAt);
    query.addBindValue(record.favorite);
    query.addBindValue(record.read);
    query.addBindValue(record.sent);
    query.addBindValue(record.local);
    query.addBindValue(record.expired);
    if (!query.exec()) {
        if (error) *error = query.lastError().text();
        return false;
    }
    return true;
}

bool HistoryRepository::containsMessage(const QString &messageId) const {
    QSqlQuery query(database_);
    query.prepare(QStringLiteral(
        "SELECT 1 FROM clipboard_history WHERE message_id=? LIMIT 1"));
    query.addBindValue(messageId);
    return query.exec() && query.next();
}

QVector<HistoryRecord> HistoryRepository::search(const QString &queryText,
                                                 const QString &deviceId,
                                                 int limit) const {
    QString sql = QStringLiteral(
        "SELECT id,message_id,content,content_hash,source_device,target_device,"
        "created_at,received_at,favorite,is_read,sent,local_created,expired "
        "FROM clipboard_history WHERE 1=1");
    if (!queryText.isEmpty()) sql += QStringLiteral(" AND content LIKE ?");
    if (!deviceId.isEmpty()) {
        sql += QStringLiteral(" AND (source_device=? OR target_device=?)");
    }
    sql += QStringLiteral(" ORDER BY favorite DESC,created_at DESC LIMIT ?");
    QSqlQuery query(database_);
    query.prepare(sql);
    if (!queryText.isEmpty()) query.addBindValue(QStringLiteral("%") + queryText +
                                                  QStringLiteral("%"));
    if (!deviceId.isEmpty()) {
        query.addBindValue(deviceId);
        query.addBindValue(deviceId);
    }
    query.addBindValue(qBound(1, limit, 5000));
    QVector<HistoryRecord> records;
    if (!query.exec()) return records;
    while (query.next()) {
        HistoryRecord r;
        r.id = query.value(0).toLongLong();
        r.messageId = query.value(1).toString();
        r.content = query.value(2).toString();
        r.contentHash = query.value(3).toByteArray();
        r.sourceDevice = query.value(4).toString();
        r.targetDevice = query.value(5).toString();
        r.createdAt = query.value(6).toLongLong();
        r.receivedAt = query.value(7).toLongLong();
        r.favorite = query.value(8).toBool();
        r.read = query.value(9).toBool();
        r.sent = query.value(10).toBool();
        r.local = query.value(11).toBool();
        r.expired = query.value(12).toBool();
        records.push_back(r);
    }
    return records;
}

bool HistoryRepository::setFavorite(qint64 id, bool favorite) {
    QSqlQuery query(database_);
    query.prepare(QStringLiteral(
        "UPDATE clipboard_history SET favorite=? WHERE id=?"));
    query.addBindValue(favorite);
    query.addBindValue(id);
    return query.exec();
}

bool HistoryRepository::markSent(const QString &messageId) {
    QSqlQuery query(database_);
    query.prepare(QStringLiteral(
        "UPDATE clipboard_history SET sent=1 WHERE message_id=?"));
    query.addBindValue(messageId);
    return query.exec();
}

bool HistoryRepository::remove(qint64 id) {
    QSqlQuery query(database_);
    query.prepare(QStringLiteral("DELETE FROM clipboard_history WHERE id=?"));
    query.addBindValue(id);
    return query.exec();
}

bool HistoryRepository::clear() {
    QSqlQuery query(database_);
    return query.exec(QStringLiteral("DELETE FROM clipboard_history"));
}

bool HistoryRepository::cleanup(int retentionDays, int maxItems) {
    const qint64 cutoff = QDateTime::currentMSecsSinceEpoch() -
                          static_cast<qint64>(qMax(1, retentionDays)) * 86400000;
    QSqlQuery expired(database_);
    expired.prepare(QStringLiteral(
        "DELETE FROM clipboard_history WHERE favorite=0 AND created_at<?"));
    expired.addBindValue(cutoff);
    if (!expired.exec()) return false;
    QSqlQuery excess(database_);
    excess.prepare(QStringLiteral(
        "DELETE FROM clipboard_history WHERE favorite=0 AND id NOT IN("
        "SELECT id FROM clipboard_history ORDER BY favorite DESC,created_at DESC LIMIT ?)"));
    excess.addBindValue(qMax(10, maxItems));
    return excess.exec();
}

