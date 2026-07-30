#include "WebSocketClient.h"

#include "CryptoManager.h"
#include "SettingsRepository.h"

#include <QDateTime>
#include <QJsonDocument>
#include <QJsonObject>
#include <QNetworkRequest>
#include <QRandomGenerator>
#include <QWebSocketHandshakeOptions>

WebSocketClient::WebSocketClient(SettingsRepository *settings, QObject *parent)
    : QObject(parent), settings_(settings) {
    reconnectTimer_.setSingleShot(true);
    connectTimeoutTimer_.setSingleShot(true);
    connectTimeoutTimer_.setInterval(15000);
    heartbeatTimer_.setInterval(20000);
    resendTimer_.setInterval(1000);
    connect(&reconnectTimer_, &QTimer::timeout, this, &WebSocketClient::open);
    connect(&connectTimeoutTimer_, &QTimer::timeout, this, [this] {
        if (socket_.state() == QAbstractSocket::ConnectingState) {
            socket_.abort();
            emit stateChanged(QStringLiteral("连接超时"));
        }
    });
    connect(&heartbeatTimer_, &QTimer::timeout, this, &WebSocketClient::heartbeat);
    connect(&resendTimer_, &QTimer::timeout, this, &WebSocketClient::resendPending);
    connect(&socket_, &QWebSocket::connected, this, &WebSocketClient::onConnected);
    connect(&socket_, &QWebSocket::disconnected, this, &WebSocketClient::onDisconnected);
    connect(&socket_, &QWebSocket::textMessageReceived, this,
            &WebSocketClient::onTextMessage);
    connect(&socket_, qOverload<QAbstractSocket::SocketError>(&QWebSocket::error),
            this, [this](QAbstractSocket::SocketError) {
                emit stateChanged(QStringLiteral("连接错误：") + socket_.errorString());
            });
}

WebSocketClient::~WebSocketClient() { disconnectFromServer(); }

void WebSocketClient::connectToServer(const QString &accessToken,
                                      const QString &deviceId) {
    accessToken_ = accessToken;
    deviceId_ = deviceId;
    intentionalClose_ = false;
    reconnectAttempt_ = 0;
    open();
}

void WebSocketClient::updateToken(const QString &accessToken) {
    accessToken_ = accessToken;
    if (socket_.state() == QAbstractSocket::ConnectedState) {
        const QJsonObject auth{{QStringLiteral("type"), QStringLiteral("auth")},
                               {QStringLiteral("access_token"), accessToken_},
                               {QStringLiteral("device_id"), deviceId_},
                               {QStringLiteral("last_sequence"), 0}};
        socket_.sendTextMessage(
            QString::fromUtf8(QJsonDocument(auth).toJson(QJsonDocument::Compact)));
    }
}

void WebSocketClient::open() {
    if (intentionalClose_ || accessToken_.isEmpty() || deviceId_.isEmpty() ||
        socket_.state() == QAbstractSocket::ConnectingState ||
        socket_.state() == QAbstractSocket::ConnectedState) {
        return;
    }
    QUrl url = settings_->serverUrl();
    url.setScheme(url.scheme() == QStringLiteral("http") ? QStringLiteral("ws")
                                                          : QStringLiteral("wss"));
    url.setPath(QStringLiteral("/api/v1/ws"));
    QNetworkRequest request(url);
    request.setTransferTimeout(15000);
    request.setRawHeader("User-Agent", "ClipBridge-Desktop/0.3");
    QWebSocketHandshakeOptions options;
    options.setSubprotocols({QStringLiteral("clipbridge.v1")});
    emit stateChanged(QStringLiteral("正在连接"));
    socket_.open(request, options);
    connectTimeoutTimer_.start();
}

void WebSocketClient::onConnected() {
    connectTimeoutTimer_.stop();
    authenticated_ = false;
    reconnectAttempt_ = 0;
    emit stateChanged(QStringLiteral("正在认证"));
    updateToken(accessToken_);
}

void WebSocketClient::onDisconnected() {
    connectTimeoutTimer_.stop();
    authenticated_ = false;
    heartbeatTimer_.stop();
    resendTimer_.stop();
    emit stateChanged(QStringLiteral("已断开"));
    if (!intentionalClose_) scheduleReconnect();
}

void WebSocketClient::disconnectFromServer() {
    intentionalClose_ = true;
    reconnectTimer_.stop();
    connectTimeoutTimer_.stop();
    heartbeatTimer_.stop();
    resendTimer_.stop();
    if (socket_.state() != QAbstractSocket::UnconnectedState) {
        socket_.close(QWebSocketProtocol::CloseCodeNormal,
                      QStringLiteral("客户端退出"));
    }
}

void WebSocketClient::scheduleReconnect() {
    const int base = qMin(60000, 1000 * (1 << qMin(reconnectAttempt_, 5)));
    const int jitter = QRandomGenerator::global()->bounded(qMax(1, base / 5));
    reconnectTimer_.start(base + jitter);
    reconnectAttempt_++;
    emit stateChanged(QStringLiteral("等待重连"));
}

bool WebSocketClient::sendEnvelope(const Envelope &envelope) {
    const QString json = QString::fromUtf8(
        QJsonDocument(toJson(envelope)).toJson(QJsonDocument::Compact));
    Pending pending{json, 1, QDateTime::currentMSecsSinceEpoch() + 1000};
    pending_.insert(envelope.messageId, pending);
    if (!authenticated_) return false;
    return socket_.sendTextMessage(json) > 0;
}

void WebSocketClient::sendAck(const QString &messageId, const QString &status) {
    const QJsonObject ack{{QStringLiteral("type"), QStringLiteral("message_ack")},
                          {QStringLiteral("message_id"), messageId},
                          {QStringLiteral("status"), status}};
    socket_.sendTextMessage(
        QString::fromUtf8(QJsonDocument(ack).toJson(QJsonDocument::Compact)));
}

bool WebSocketClient::isAuthenticated() const { return authenticated_; }

void WebSocketClient::heartbeat() {
    if (!authenticated_) return;
    const QJsonObject frame{{QStringLiteral("type"), QStringLiteral("heartbeat")}};
    socket_.sendTextMessage(
        QString::fromUtf8(QJsonDocument(frame).toJson(QJsonDocument::Compact)));
    socket_.ping();
}

void WebSocketClient::resendPending() {
    if (!authenticated_) return;
    const qint64 now = QDateTime::currentMSecsSinceEpoch();
    for (auto it = pending_.begin(); it != pending_.end(); ++it) {
        if (it->nextAttempt > now) continue;
        socket_.sendTextMessage(it->json);
        const int seconds = qMin(30, 1 << qMin(it->attempts, 5));
        it->attempts++;
        it->nextAttempt = now + seconds * 1000;
    }
}

void WebSocketClient::onTextMessage(const QString &message) {
    const QJsonObject object = QJsonDocument::fromJson(message.toUtf8()).object();
    const QString type = object.value(QStringLiteral("type")).toString();
    if (type == QStringLiteral("auth_ok")) {
        authenticated_ = true;
        heartbeatTimer_.start();
        resendTimer_.start();
        emit stateChanged(QStringLiteral("已连接"));
        emit authenticated();
    } else if (type == QStringLiteral("clipboard_text")) {
        emit envelopeReceived(fromJson(object));
    } else if (type == QStringLiteral("message_ack")) {
        const QString id = object.value(QStringLiteral("message_id")).toString();
        pending_.remove(id);
        emit messageAcknowledged(id);
    } else if (type == QStringLiteral("device_online") ||
               type == QStringLiteral("device_offline")) {
        emit deviceOnlineChanged(object.value(QStringLiteral("device_id")).toString(),
                                 type == QStringLiteral("device_online"));
    } else if (type == QStringLiteral("device_revoked")) {
        emit deviceRevoked(object.value(QStringLiteral("device_id")).toString());
    } else if (type == QStringLiteral("auth_error") ||
               type == QStringLiteral("error")) {
        emit protocolError(object.value(QStringLiteral("code")).toString());
    }
}

QJsonObject WebSocketClient::toJson(const Envelope &e) {
    return {{QStringLiteral("version"), e.version},
            {QStringLiteral("type"), e.type},
            {QStringLiteral("message_id"), e.messageId},
            {QStringLiteral("sender_device_id"), e.senderDeviceId},
            {QStringLiteral("recipient_device_id"), e.recipientDeviceId},
            {QStringLiteral("created_at"), e.createdAt},
            {QStringLiteral("expires_at"), e.expiresAt},
            {QStringLiteral("nonce"),
             QString::fromLatin1(CryptoManager::toBase64Url(e.nonce))},
            {QStringLiteral("ciphertext"),
             QString::fromLatin1(CryptoManager::toBase64Url(e.ciphertext))},
            {QStringLiteral("signature"),
             QString::fromLatin1(CryptoManager::toBase64Url(e.signature))},
            {QStringLiteral("offline_allowed"), e.offlineAllowed}};
}

Envelope WebSocketClient::fromJson(const QJsonObject &object) {
    Envelope e;
    e.version = object.value(QStringLiteral("version")).toInt();
    e.type = object.value(QStringLiteral("type")).toString();
    e.messageId = object.value(QStringLiteral("message_id")).toString();
    e.senderDeviceId = object.value(QStringLiteral("sender_device_id")).toString();
    e.recipientDeviceId = object.value(QStringLiteral("recipient_device_id")).toString();
    e.createdAt = object.value(QStringLiteral("created_at")).toInteger();
    e.expiresAt = object.value(QStringLiteral("expires_at")).toInteger();
    e.nonce = CryptoManager::fromBase64Url(
        object.value(QStringLiteral("nonce")).toString());
    e.ciphertext = CryptoManager::fromBase64Url(
        object.value(QStringLiteral("ciphertext")).toString());
    e.signature = CryptoManager::fromBase64Url(
        object.value(QStringLiteral("signature")).toString());
    e.sequence = object.value(QStringLiteral("sequence")).toInteger();
    return e;
}
