#pragma once

#include "Models.h"

#include <QHash>
#include <QObject>
#include <QTimer>
#include <QWebSocket>

class SettingsRepository;

class WebSocketClient final : public QObject {
    Q_OBJECT

public:
    explicit WebSocketClient(SettingsRepository *settings, QObject *parent = nullptr);
    ~WebSocketClient() override;

    void connectToServer(const QString &accessToken, const QString &deviceId);
    void updateToken(const QString &accessToken);
    void disconnectFromServer();
    bool sendEnvelope(const Envelope &envelope);
    void sendAck(const QString &messageId, const QString &status = QStringLiteral("processed"));
    [[nodiscard]] bool isAuthenticated() const;

signals:
    void stateChanged(const QString &state);
    void authenticated();
    void envelopeReceived(const Envelope &envelope);
    void messageAcknowledged(const QString &messageId);
    void deviceOnlineChanged(const QString &deviceId, bool online);
    void deviceRevoked(const QString &deviceId);
    void protocolError(const QString &message);

private slots:
    void open();
    void onConnected();
    void onDisconnected();
    void onTextMessage(const QString &message);
    void heartbeat();
    void resendPending();

private:
    struct Pending {
        QString json;
        int attempts{0};
        qint64 nextAttempt{0};
    };
    static QJsonObject toJson(const Envelope &envelope);
    static Envelope fromJson(const QJsonObject &object);
    void scheduleReconnect();

    SettingsRepository *settings_;
    QWebSocket socket_;
    QTimer reconnectTimer_;
    QTimer connectTimeoutTimer_;
    QTimer heartbeatTimer_;
    QTimer resendTimer_;
    QString accessToken_;
    QString deviceId_;
    bool authenticated_{false};
    bool intentionalClose_{false};
    int reconnectAttempt_{0};
    QHash<QString, Pending> pending_;
};
