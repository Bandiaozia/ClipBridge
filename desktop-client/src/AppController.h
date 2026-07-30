#pragma once

#include <QObject>
#include <QString>
#include <QTimer>

class ApiClient;
class AndroidUsbConnector;
class AutoStartManager;
class ClipboardManager;
class ClipboardSyncService;
class CryptoManager;
class DeviceManager;
class HistoryRepository;
class NotificationManager;
class PairingManager;
class QCheckBox;
class QLabel;
class QListWidget;
class QMainWindow;
class QTextEdit;
class QWidget;
class SensitiveContentDetector;
class SettingsRepository;
class TokenManager;
class WebSocketClient;

class AppController final : public QObject {
    Q_OBJECT
    Q_PROPERTY(QString connectionStatus READ connectionStatus
               NOTIFY connectionStatusChanged)

public:
    explicit AppController(QObject *parent = nullptr);
    ~AppController() override;

    [[nodiscard]] QString connectionStatus() const;
    QWidget *createMainWindow();
    void start();

signals:
    void connectionStatusChanged();

private:
    void showLoginDialog();
    void showHistoryDialog();
    void showSettingsDialog();
    void showPairingDialog();
    void refreshDeviceList();
    void beginSession();
    void setConnectionStatus(const QString &status);
    void applyTheme();
    void logout();

    QString connectionStatus_{QStringLiteral("尚未连接")};
    SettingsRepository *settings_;
    AutoStartManager *autoStart_;
    AndroidUsbConnector *androidUsb_;
    CryptoManager *crypto_;
    DeviceManager *devices_;
    HistoryRepository *history_;
    TokenManager *tokens_;
    ApiClient *api_;
    WebSocketClient *websocket_;
    ClipboardManager *clipboard_;
    SensitiveContentDetector *sensitive_;
    ClipboardSyncService *sync_;
    NotificationManager *notifications_{nullptr};
    PairingManager *pairing_;
    QMainWindow *window_{nullptr};
    QLabel *statusLabel_{nullptr};
    QListWidget *deviceList_{nullptr};
    QTextEdit *recentText_{nullptr};
    QCheckBox *syncCheck_{nullptr};
    QTimer tokenRefreshTimer_;
    bool exiting_{false};
};
