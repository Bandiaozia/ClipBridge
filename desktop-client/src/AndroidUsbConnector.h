#pragma once

#include <QObject>
#include <QString>
#include <QStringList>

class QProcess;
class QTimer;

/**
 * 通过用户已经授权的 ADB 启动 ClipBridge 自有 shell 剪贴板桥接。
 *
 * 桥接只监听 127.0.0.1，并使用每次启动生成的随机令牌认证普通应用进程。
 * 它不会绕过 Android 的 USB 调试授权，手机重启后需要用户再次点击恢复。
 */
class AndroidUsbConnector final : public QObject {
    Q_OBJECT

public:
    explicit AndroidUsbConnector(QObject *parent = nullptr);

    void refresh();
    void activateClipBridge();

    [[nodiscard]] QString status() const;
    [[nodiscard]] static QStringList parseAuthorizedSerials(const QByteArray &output);

signals:
    void statusChanged(const QString &status);
    void activationFinished(bool success, const QString &message);

private:
    enum class Operation {
        None,
        Detect,
        CheckClipBridgePackage,
        StopOldBridge,
        StartBridge,
        LaunchApp,
        VerifyBridge,
    };

    void start(Operation operation, const QStringList &arguments);
    void setStatus(const QString &status);
    [[nodiscard]] QString findAdb() const;
    void handleFinished(int exitCode);

    QProcess *process_;
    QTimer *timeout_;
    QString adbPath_;
    QString serial_;
    QString apkPath_;
    QString token_;
    QString status_{QStringLiteral("尚未检测 USB 手机")};
    Operation operation_{Operation::None};
    bool activateAfterDetection_{false};
};
