#include "AppController.h"

#include <QApplication>
#include <QCoreApplication>
#include <QCryptographicHash>
#include <QDir>
#include <QLockFile>
#include <QLocalServer>
#include <QLocalSocket>
#include <QMessageBox>
#include <QStandardPaths>
#include <QTimer>
#include <QWidget>

int main(int argc, char *argv[]) {
    QApplication app(argc, argv);
    QCoreApplication::setApplicationName(QStringLiteral("ClipBridge"));
    QCoreApplication::setOrganizationName(QStringLiteral("ClipBridge"));
    QCoreApplication::setApplicationVersion(QStringLiteral("0.3.0"));
    QGuiApplication::setDesktopFileName(QStringLiteral("clipbridge"));

    // GNOME/Wayland/X11 都可能拒绝后台进程抢焦点。再次点击应用时，与其
    // 让旧进程请求激活，不如让携带本次真实用户激活令牌的新进程接管。
    // 这样既保持单实例，也不会出现“后台有进程但窗口打不开”。
    const QByteArray userScope =
        QCryptographicHash::hash(QDir::homePath().toUtf8(),
                                 QCryptographicHash::Sha256)
            .toHex()
            .left(16);
    const QString localServerName =
        QStringLiteral("clipbridge-desktop-%1")
            .arg(QString::fromLatin1(userScope));

    QString runtimeDirectory =
        QStandardPaths::writableLocation(QStandardPaths::RuntimeLocation);
    if (runtimeDirectory.isEmpty()) {
        runtimeDirectory =
            QStandardPaths::writableLocation(QStandardPaths::TempLocation);
    }
    QDir().mkpath(runtimeDirectory);
    QLockFile instanceLock(
        QDir(runtimeDirectory).filePath(QStringLiteral("clipbridge-desktop.lock")));
    instanceLock.setStaleLockTime(0);

    bool replacingExistingInstance = false;
    QLocalSocket activationSocket;
    activationSocket.connectToServer(localServerName, QIODevice::ReadWrite);
    if (activationSocket.waitForConnected(300)) {
        activationSocket.write("RELAUNCH");
        activationSocket.flush();
        activationSocket.waitForBytesWritten(300);
        activationSocket.waitForReadyRead(1000);
        activationSocket.disconnectFromServer();
        replacingExistingInstance = true;
    }

    const bool locked = replacingExistingInstance
                            ? instanceLock.tryLock(5000)
                            : instanceLock.tryLock();
    if (!locked) {
        QMessageBox::information(
            nullptr, QStringLiteral("ClipBridge"),
            QStringLiteral("无法接管正在运行的 ClipBridge，请稍后重试。"));
        return 0;
    }

    QApplication::setQuitOnLastWindowClosed(true);
    AppController controller;
    QWidget *window = controller.createMainWindow();

    QLocalServer activationServer;
    activationServer.setSocketOptions(QLocalServer::UserAccessOption);
    QLocalServer::removeServer(localServerName);
    if (!activationServer.listen(localServerName)) {
        QMessageBox::warning(
            window, QStringLiteral("ClipBridge"),
            QStringLiteral("无法创建窗口唤醒通道：%1")
                .arg(activationServer.errorString()));
    }
    QObject::connect(&activationServer, &QLocalServer::newConnection, window,
                     [&activationServer, window] {
                         bool shouldRelaunch = false;
                         while (QLocalSocket *socket =
                                    activationServer.nextPendingConnection()) {
                             if (socket->bytesAvailable() == 0) {
                                 socket->waitForReadyRead(1000);
                             }
                             const QByteArray command = socket->readAll();
                             shouldRelaunch =
                                 shouldRelaunch || command.contains("RELAUNCH");
                             socket->write("OK");
                             socket->flush();
                             socket->disconnectFromServer();
                             socket->deleteLater();
                         }
                         if (shouldRelaunch) {
                             if (window->isHidden()) {
                                 window->show();
                                 window->raise();
                                 window->activateWindow();
                             }
                         }
                     });

    // Mutter 在尝试装饰 Qt 窗口时存在 bug，会将窗口立即最小化后取消映射。
    // 使用无边框窗口绕过 Mutter 装饰逻辑，但仍为受管窗口。
#if defined(Q_OS_LINUX)
    if (!qEnvironmentVariableIsSet("CLIPBRIDGE_MANAGED_WINDOW")) {
        window->setWindowFlags(Qt::FramelessWindowHint | Qt::Window);
    }
#endif
    window->show();
    controller.start();
    return app.exec();
}
